// Package scripts embeds all Lua rate-limiting scripts and exposes them
// as ready-to-use *valkey.Lua values. Each algorithm's implementation
// imports this package and uses the corresponding script variable.
package scripts

import (
	_ "embed"

	"github.com/valkey-io/valkey-go"
)

//go:embed leaky_bucket.lua
var luaLeakyBucket string

// LeakyBucket is the Lua script for the leaky-bucket algorithm.
var LeakyBucket = valkey.NewLuaScript(luaLeakyBucket)

//go:embed token_bucket.lua
var luaTokenBucket string

// TokenBucket is the Lua script for the token-bucket algorithm.
var TokenBucket = valkey.NewLuaScript(luaTokenBucket)

//go:embed fixed_window.lua
var luaFixedWindow string

// FixedWindow is the Lua script for the fixed-window counter algorithm.
var FixedWindow = valkey.NewLuaScript(luaFixedWindow)

//go:embed sliding_window_counter.lua
var luaSlidingWindowCounter string

// SlidingWindowCounter is the Lua script for the sliding-window counter algorithm.
var SlidingWindowCounter = valkey.NewLuaScript(luaSlidingWindowCounter)

//go:embed sliding_window_log.lua
var luaSlidingWindowLog string

// SlidingWindowLog is the Lua script for the sliding-window log algorithm.
var SlidingWindowLog = valkey.NewLuaScript(luaSlidingWindowLog)
