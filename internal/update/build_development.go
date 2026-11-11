//go:build !production

package update

// production reports whether this is a release build. Only those update
// themselves: a development build would be replaced by a release.
const production = false
