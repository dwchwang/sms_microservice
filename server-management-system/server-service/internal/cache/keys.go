package cache

// ListVersionKey holds the counter that invalidates list and detail caches.
// Bumped by mutations and by applied status events.
const ListVersionKey = "server:list:version"
