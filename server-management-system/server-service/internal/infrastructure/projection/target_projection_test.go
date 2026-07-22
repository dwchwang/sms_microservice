package projection

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
)

type call struct {
	op   string
	key  string
	args []any
}

// fakeTargetOps records calls and keeps just enough state to model the set,
// the hashes and the ready marker.
type fakeTargetOps struct {
	calls   []call
	failOn  string
	sets    map[string]map[string]bool // set key -> members
	hashes  map[string]bool            // hash key -> exists
	strings map[string]string
}

func newFakeTargetOps(failOn string) *fakeTargetOps {
	return &fakeTargetOps{
		failOn:  failOn,
		sets:    make(map[string]map[string]bool),
		hashes:  make(map[string]bool),
		strings: make(map[string]string),
	}
}

func (f *fakeTargetOps) record(op, key string, args ...any) error {
	f.calls = append(f.calls, call{op: op, key: key, args: args})
	if f.failOn == op {
		return errors.New("redis down")
	}
	return nil
}

func (f *fakeTargetOps) WriteTargets(ctx context.Context, idsKey string, targets []Target) error {
	if err := f.record("WRITE", idsKey, len(targets)); err != nil {
		return err
	}
	if f.sets[idsKey] == nil {
		f.sets[idsKey] = make(map[string]bool)
	}
	for _, t := range targets {
		f.hashes[targetKey(t.ServerID)] = true
		f.sets[idsKey][t.ServerID] = true
	}
	return nil
}

func (f *fakeTargetOps) Rename(ctx context.Context, src, dst string) error {
	if err := f.record("RENAME", src, dst); err != nil {
		return err
	}
	if _, ok := f.sets[src]; !ok {
		return errors.New("no such key")
	}
	f.sets[dst] = f.sets[src]
	delete(f.sets, src)
	return nil
}

func (f *fakeTargetOps) Set(ctx context.Context, key, value string) error {
	if err := f.record("SET", key, value); err != nil {
		return err
	}
	f.strings[key] = value
	return nil
}

func (f *fakeTargetOps) ScanTargetHashes(ctx context.Context, cursor uint64, count int64) ([]string, uint64, error) {
	if err := f.record("SCAN", targetKeyPrefix+"*"); err != nil {
		return nil, 0, err
	}
	keys := make([]string, 0, len(f.hashes))
	for k := range f.hashes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, 0, nil
}

func (f *fakeTargetOps) SIsMember(ctx context.Context, key, member string) (bool, error) {
	if err := f.record("SISMEMBER", key, member); err != nil {
		return false, err
	}
	return f.sets[key][member], nil
}

func (f *fakeTargetOps) HSet(ctx context.Context, key string, values ...any) error {
	return f.record("HSET", key, values...)
}

func (f *fakeTargetOps) SAdd(ctx context.Context, key string, members ...any) error {
	return f.record("SADD", key, members...)
}

func (f *fakeTargetOps) SRem(ctx context.Context, key string, members ...any) error {
	return f.record("SREM", key, members...)
}

func (f *fakeTargetOps) ZRem(ctx context.Context, key string, members ...any) error {
	return f.record("ZREM", key, members...)
}

func (f *fakeTargetOps) Del(ctx context.Context, keys ...string) error {
	if err := f.record("DEL", keys[0], anySlice(keys[1:])...); err != nil {
		return err
	}
	for _, k := range keys {
		delete(f.hashes, k)
		delete(f.sets, k)
	}
	return nil
}

func anySlice(in []string) []any {
	out := make([]any, 0, len(in))
	for _, s := range in {
		out = append(out, s)
	}
	return out
}

func (f *fakeTargetOps) ops() []string {
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.op)
	}
	return out
}

func newTestProjection(failOn string) (*targetProjection, *fakeTargetOps) {
	fake := newFakeTargetOps(failOn)
	return &targetProjection{ops: fake}, fake
}

// fakeSource pages through a fixed list of targets, cursored on ServerID.
type fakeSource struct {
	targets []Target
	err     error
}

func (s *fakeSource) NextTargets(ctx context.Context, cursor string, limit int) ([]Target, error) {
	if s.err != nil {
		return nil, s.err
	}
	var out []Target
	for _, t := range s.targets {
		if t.ServerID > cursor {
			out = append(out, t)
		}
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func TestSync_WritesHashBeforeID(t *testing.T) {
	p, fake := newTestProjection("")

	if err := p.Sync(context.Background(), Target{ServerID: "SRV-001", ServerName: "web-01", IPv4: "10.0.0.1", TCPPort: 8080}); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if got := strings.Join(fake.ops(), ","); got != "HSET,SADD" {
		t.Fatalf("expected HSET before SADD, got %s", got)
	}
	hset := fake.calls[0]
	if hset.key != "server:monitor-target:SRV-001" {
		t.Errorf("unexpected hash key %q", hset.key)
	}
	want := []any{"server_name", "web-01", "ipv4", "10.0.0.1", "tcp_port", "8080"}
	if len(hset.args) != len(want) {
		t.Fatalf("HSET args = %v, want %v", hset.args, want)
	}
	for i := range want {
		if hset.args[i] != want[i] {
			t.Fatalf("HSET args = %v, want %v", hset.args, want)
		}
	}
	if fake.calls[1].key != "server:monitor-target:ids" {
		t.Errorf("unexpected set key %q", fake.calls[1].key)
	}
}

func TestDelete_RemovesIDBeforeHash(t *testing.T) {
	p, fake := newTestProjection("")

	if err := p.Delete(context.Background(), "SRV-001"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// SREM first so Monitoring cannot pick the target up again, then the hash,
	// then the uptime index and status so a deleted server stops scoring.
	if got := strings.Join(fake.ops(), ","); got != "SREM,DEL,ZREM,DEL" {
		t.Fatalf("expected SREM,DEL,ZREM,DEL, got %s", got)
	}
	if fake.calls[0].key != "server:monitor-target:ids" {
		t.Errorf("unexpected set key %q", fake.calls[0].key)
	}
	if fake.calls[1].key != "server:monitor-target:SRV-001" {
		t.Errorf("unexpected hash key %q", fake.calls[1].key)
	}
	if fake.calls[2].key != "monitor:uptime:index" {
		t.Errorf("unexpected uptime index key %q", fake.calls[2].key)
	}
	if fake.calls[3].key != "monitor:status:SRV-001" {
		t.Errorf("unexpected status key %q", fake.calls[4].key)
	}
}

// A failed HSET must not add the ID, or Monitoring would read a target with no metadata.
func TestSync_DoesNotAddIDWhenHashFails(t *testing.T) {
	p, fake := newTestProjection("HSET")

	if err := p.Sync(context.Background(), Target{ServerID: "SRV-001", ServerName: "web-01", IPv4: "10.0.0.1", TCPPort: 8080}); err == nil {
		t.Fatal("expected an error when HSET fails")
	}

	if got := strings.Join(fake.ops(), ","); got != "HSET" {
		t.Fatalf("expected SADD to be skipped, got %s", got)
	}
}

// A failed SREM must not delete the hash, or the ID would point at nothing.
func TestDelete_DoesNotDeleteHashWhenSRemFails(t *testing.T) {
	p, fake := newTestProjection("SREM")

	if err := p.Delete(context.Background(), "SRV-001"); err == nil {
		t.Fatal("expected an error when SREM fails")
	}

	if got := strings.Join(fake.ops(), ","); got != "SREM" {
		t.Fatalf("expected DEL to be skipped, got %s", got)
	}
}

func TestSync_ReturnsErrorWhenSAddFails(t *testing.T) {
	p, _ := newTestProjection("SADD")

	if err := p.Sync(context.Background(), Target{ServerID: "SRV-001", ServerName: "web-01", IPv4: "10.0.0.1", TCPPort: 8080}); err == nil {
		t.Fatal("expected an error when SADD fails")
	}
}

func TestRebuild_WritesTargetsAndMarksReady(t *testing.T) {
	p, fake := newTestProjection("")
	src := &fakeSource{targets: []Target{
		{ServerID: "SRV-001", ServerName: "web-01", IPv4: "10.0.0.1", TCPPort: 8080},
		{ServerID: "SRV-002", ServerName: "web-02", IPv4: "10.0.0.2", TCPPort: 9090},
	}}

	written, err := p.Rebuild(context.Background(), src)
	if err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}

	if written != 2 {
		t.Errorf("written = %d, want 2", written)
	}
	ids := fake.sets[targetIDsKey]
	if !ids["SRV-001"] || !ids["SRV-002"] || len(ids) != 2 {
		t.Errorf("ids set = %v, want SRV-001 and SRV-002", ids)
	}
	if fake.strings[targetReadyKey] != "1" {
		t.Error("expected ready marker to be set")
	}
}

func TestRebuild_PagesThroughSource(t *testing.T) {
	p, fake := newTestProjection("")
	var targets []Target
	for i := range rebuildPageSize + 5 {
		targets = append(targets, Target{
			ServerID: fmt.Sprintf("SRV-%05d", i),
			IPv4:     "10.0.0.1",
			TCPPort:  80,
		})
	}

	written, err := p.Rebuild(context.Background(), &fakeSource{targets: targets})
	if err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}

	if written != rebuildPageSize+5 {
		t.Fatalf("written = %d, want %d", written, rebuildPageSize+5)
	}
	if len(fake.sets[targetIDsKey]) != rebuildPageSize+5 {
		t.Errorf("ids set has %d members, want %d", len(fake.sets[targetIDsKey]), rebuildPageSize+5)
	}
}

// The ID set is only swapped in at the end, so a reader never sees it partly built.
func TestRebuild_SwapsIDSetInAtomically(t *testing.T) {
	p, fake := newTestProjection("")
	src := &fakeSource{targets: []Target{{ServerID: "SRV-001", IPv4: "10.0.0.1", TCPPort: 80}}}

	if _, err := p.Rebuild(context.Background(), src); err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}

	var renameIdx, setReadyIdx = -1, -1
	for i, c := range fake.calls {
		switch {
		case c.op == "RENAME":
			renameIdx = i
		case c.op == "SET" && c.key == targetReadyKey:
			setReadyIdx = i
		case c.op == "WRITE" && c.key == targetIDsKey:
			t.Fatal("targets were written straight into the live ids key")
		}
	}
	if renameIdx == -1 {
		t.Fatal("expected a RENAME into the live ids key")
	}
	if setReadyIdx < renameIdx {
		t.Error("ready marker was set before the ids swap")
	}
}

// An empty source must clear the set rather than RENAME a key that was never created.
func TestRebuild_EmptySourceClearsIDSet(t *testing.T) {
	p, fake := newTestProjection("")
	fake.sets[targetIDsKey] = map[string]bool{"SRV-OLD": true}

	written, err := p.Rebuild(context.Background(), &fakeSource{})
	if err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}

	if written != 0 {
		t.Errorf("written = %d, want 0", written)
	}
	if len(fake.sets[targetIDsKey]) != 0 {
		t.Errorf("expected ids set cleared, got %v", fake.sets[targetIDsKey])
	}
	if fake.strings[targetReadyKey] != "1" {
		t.Error("expected ready marker to be set even with no targets")
	}
}

func TestRebuild_DeletesOrphanHashes(t *testing.T) {
	p, fake := newTestProjection("")
	fake.hashes[targetKey("SRV-GONE")] = true

	if _, err := p.Rebuild(context.Background(), &fakeSource{
		targets: []Target{{ServerID: "SRV-001", IPv4: "10.0.0.1", TCPPort: 80}},
	}); err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}

	if fake.hashes[targetKey("SRV-GONE")] {
		t.Error("expected orphan hash to be deleted")
	}
	if !fake.hashes[targetKey("SRV-001")] {
		t.Error("expected live target hash to survive")
	}
}

func TestRebuild_SourceErrorLeavesLiveSetIntact(t *testing.T) {
	p, fake := newTestProjection("")
	fake.sets[targetIDsKey] = map[string]bool{"SRV-OLD": true}

	if _, err := p.Rebuild(context.Background(), &fakeSource{err: errors.New("db down")}); err == nil {
		t.Fatal("expected an error when the source fails")
	}

	if !fake.sets[targetIDsKey]["SRV-OLD"] {
		t.Error("expected the live ids set to survive a failed rebuild")
	}
	if fake.strings[targetReadyKey] == "1" {
		t.Error("expected ready marker not to be set after a failed rebuild")
	}
}
