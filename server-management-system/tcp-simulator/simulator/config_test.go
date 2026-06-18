package simulator

import (
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("SIMULATOR_BASE_PORT", "")
	t.Setenv("SIMULATOR_NUM_SERVERS", "")
	t.Setenv("SIMULATOR_TICK_INTERVAL", "")
	t.Setenv("SIMULATOR_DEFAULT_UPTIME_RATE", "")

	cfg := LoadConfig()
	if cfg.BasePort != 9001 || cfg.NumServers != 10000 || cfg.TickInterval != 30*time.Second || cfg.DefaultUptime != 0.95 {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
}

func TestLoadConfig_FromEnvironment(t *testing.T) {
	t.Setenv("SIMULATOR_BASE_PORT", "19001")
	t.Setenv("SIMULATOR_NUM_SERVERS", "25")
	t.Setenv("SIMULATOR_TICK_INTERVAL", "250ms")
	t.Setenv("SIMULATOR_DEFAULT_UPTIME_RATE", "0.75")

	cfg := LoadConfig()
	if cfg.BasePort != 19001 || cfg.NumServers != 25 || cfg.TickInterval != 250*time.Millisecond || cfg.DefaultUptime != 0.75 {
		t.Fatalf("unexpected env config: %#v", cfg)
	}
}

func TestLoadConfig_InvalidEnvironmentFallsBackToDefaults(t *testing.T) {
	t.Setenv("SIMULATOR_BASE_PORT", "bad")
	t.Setenv("SIMULATOR_NUM_SERVERS", "bad")
	t.Setenv("SIMULATOR_TICK_INTERVAL", "bad")
	t.Setenv("SIMULATOR_DEFAULT_UPTIME_RATE", "bad")

	cfg := LoadConfig()
	if cfg.BasePort != 9001 || cfg.NumServers != 10000 || cfg.TickInterval != 30*time.Second || cfg.DefaultUptime != 0.95 {
		t.Fatalf("expected defaults for invalid env, got %#v", cfg)
	}
}

func TestIntToStr(t *testing.T) {
	tests := map[int]string{
		0:     "0",
		42:    "42",
		-1901: "-1901",
	}
	for input, want := range tests {
		if got := intToStr(input); got != want {
			t.Fatalf("intToStr(%d) = %q, want %q", input, got, want)
		}
	}
}
