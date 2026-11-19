package service

import (
	"net/http"
	"regexp"
	"strings"
)

// RegistryOp is the kind of /v2 operation a path names.
type RegistryOp string

const (
	RegistryOpBlob        RegistryOp = "blob"
	RegistryOpUploadStart RegistryOp = "upload-start"
	RegistryOpUpload      RegistryOp = "upload"
	RegistryOpManifest    RegistryOp = "manifest"
	RegistryOpTags        RegistryOp = "tags"
	RegistryOpReferrers   RegistryOp = "referrers"
)

const (
	RegistryActionPull   = "pull"
	RegistryActionPush   = "push"
	RegistryActionDelete = "delete"

	maxRegistryNameLen = 255
)

// RegistryRoute is a /v2 path split from the right: the name may itself contain "blobs" or "manifests".
type RegistryRoute struct {
	Name string
	Op   RegistryOp
	// Ref is the digest, tag or upload id the path ends in; empty for upload-start and tags.
	Ref string
}

// ParseRegistryRoute splits the path after "/v2/". It reports false for a path no operation matches.
func ParseRegistryRoute(rest string) (RegistryRoute, bool) {
	segs := strings.Split(strings.TrimSuffix(rest, "/"), "/")
	n := len(segs)
	named := func(op RegistryOp, ref string, tail int) (RegistryRoute, bool) {
		if n <= tail {
			return RegistryRoute{}, false
		}
		return RegistryRoute{Name: strings.Join(segs[:n-tail], "/"), Op: op, Ref: ref}, true
	}
	switch {
	case n >= 2 && segs[n-2] == "tags" && segs[n-1] == "list":
		return named(RegistryOpTags, "", 2)
	case n >= 2 && segs[n-2] == "manifests":
		return named(RegistryOpManifest, segs[n-1], 2)
	case n >= 2 && segs[n-2] == "referrers":
		return named(RegistryOpReferrers, segs[n-1], 2)
	case n >= 3 && segs[n-3] == "blobs" && segs[n-2] == "uploads":
		return named(RegistryOpUpload, segs[n-1], 3)
	case n >= 2 && segs[n-2] == "blobs" && segs[n-1] == "uploads":
		return named(RegistryOpUploadStart, "", 2)
	case n >= 2 && segs[n-2] == "blobs":
		return named(RegistryOpBlob, segs[n-1], 2)
	}
	return RegistryRoute{}, false
}

var registryNameRe = regexp.MustCompile(`^[a-z0-9]+(?:(?:\.|_|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:\.|_|__|-+)[a-z0-9]+)*)*$`)

// ValidRegistryName applies the OCI name grammar and the length cap.
func ValidRegistryName(name string) bool {
	return len(name) <= maxRegistryNameLen && registryNameRe.MatchString(name)
}

// RegistryActionFor is the action a request method needs on a name.
func RegistryActionFor(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead:
		return RegistryActionPull
	case http.MethodDelete:
		return RegistryActionDelete
	}
	return RegistryActionPush
}
