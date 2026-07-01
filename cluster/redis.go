package cluster

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisRequestTimeout bounds how long a single Redis operation is allowed to take, so a slow or unreachable
// Redis server can't stall proxy operations (joins, quits, transfers, FindPlayer lookups) indefinitely.
const redisRequestTimeout = 5 * time.Second

// removeScript atomically deletes a player's presence record only if it's still owned by the calling
// proxy, avoiding a race where a GET-then-DEL could delete a record another proxy just re-announced for
// the same player (e.g. the player reconnected to a different proxy between the GET and the DEL).
var removeScript = redis.NewScript(`
	local v = redis.call("GET", KEYS[1])
	if v and string.sub(v, 1, string.len(ARGV[1])) == ARGV[1] then
		return redis.call("DEL", KEYS[1])
	end
	return 0
`)

// RedisBackend is a Backend implementation that stores player presence in Redis.
//
// Each player is stored under a "portal:player:<lowercased name>" key with a value of "<proxyID>|<server>"
// and a TTL. The TTL acts as a safety net: if a proxy crashes without removing its players' records (a
// graceful shutdown or quit/transfer always does), the records expire on their own instead of leaking
// forever. Callers are expected to periodically re-Announce online players (e.g. every TTL/2) to keep
// long-lived sessions from expiring.
type RedisBackend struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisBackend creates a RedisBackend connected to the Redis server at addr, verifying the connection
// before returning.
func NewRedisBackend(addr, password string, db int, ttl time.Duration) (*RedisBackend, error) {
	client := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})

	ctx, cancel := context.WithTimeout(context.Background(), redisRequestTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &RedisBackend{client: client, ttl: ttl}, nil
}

// Announce ...
func (b *RedisBackend) Announce(proxyID, playerName, serverName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), redisRequestTimeout)
	defer cancel()
	return b.client.Set(ctx, playerKey(playerName), proxyID+"|"+serverName, b.ttl).Err()
}

// Remove ...
func (b *RedisBackend) Remove(proxyID, playerName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), redisRequestTimeout)
	defer cancel()

	// The ownership check-and-delete must be atomic: a plain GET followed by DEL could race with another
	// proxy's Announce for the same player (e.g. a fast reconnect elsewhere) and delete its fresh record.
	return removeScript.Run(ctx, b.client, []string{playerKey(playerName)}, proxyID+"|").Err()
}

// Lookup ...
func (b *RedisBackend) Lookup(playerName string) (proxyID, serverName string, online bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), redisRequestTimeout)
	defer cancel()

	val, err := b.client.Get(ctx, playerKey(playerName)).Result()
	if errors.Is(err, redis.Nil) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}

	proxyID, serverName, ok := strings.Cut(val, "|")
	if !ok {
		return "", "", false, nil
	}
	return proxyID, serverName, true, nil
}

// Close ...
func (b *RedisBackend) Close() error {
	return b.client.Close()
}

func playerKey(playerName string) string {
	return "portal:player:" + strings.ToLower(playerName)
}
