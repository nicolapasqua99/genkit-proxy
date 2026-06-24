// Package ratelimit provides a fixed-window rate limiter with in-memory and
// Redis backends. Both backends implement Limiter, which is the sole
// dependency needed by callers.
package ratelimit
