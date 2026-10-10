//go:build !dev

package assets_test

// A tagless build embeds the frontend, so New must keep hashing it once.
const newIsDev = false
