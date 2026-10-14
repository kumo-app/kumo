// Package corepin holds the SHA-256 of every mihomo core Kumo ships, as
// pinned in scripts/fetch-core/core.lock.json (TestMatchesLock keeps the
// two equal). Enhanced Mode privileges a core only when its contents
// match one of these, whatever file it was found in.
package corepin

import "slices"

// Binaries are the extracted release executables, by os-arch.
var Binaries = map[string]string{
	"linux-amd64":   "7a0d59da2e678d56c899a3db996a2ad8963286c4f3634b0451435db248f13fa1",
	"linux-arm64":   "c80df2ca5159567804da883401e0118908fc1155e05df2baf3dbd4d3ecfc3754",
	"darwin-amd64":  "c666c4dd92a2c27c24ff56c9fe42e115f7de94467d45b1eb8caf88f6235e2e7b",
	"darwin-arm64":  "94a386ec0149080deadd86b1f667363bde3c70f7489dba3258e52c56fc9a6d66",
	"windows-amd64": "04f8d7fc2b314771e1ecbf15951d59f0e1cb7914503c88cfb6972ff6a824e314",
}

// Packaged are cores that packaging changes: the macOS app holds a
// universal binary of both darwin cores, signed ad hoc. CI checks the
// value against the app it builds.
var Packaged = map[string]string{
	"darwin-universal": "e76e38a3621da8385c955d13d42287c2e3987d577c986a6426a78b5cef3d9809",
}

// Trusted lists, sorted, the digests a core may have on goos.
func Trusted(goos string) []string {
	var list []string
	for _, m := range []map[string]string{Binaries, Packaged} {
		for k, v := range m {
			if len(k) > len(goos) && k[:len(goos)+1] == goos+"-" && len(v) == 64 {
				list = append(list, v)
			}
		}
	}
	slices.Sort(list)
	return list
}
