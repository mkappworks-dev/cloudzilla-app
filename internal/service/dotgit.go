package service

import "strings"

// isDotGit reports whether a path segment names the .git directory on a
// filesystem git checks out to. It mirrors the rules git applies, as ported in
// go-git's internal pathutil (IsDotGitName, IsNTFSDotGit, IsHFSDotGit).
func isDotGit(seg string) bool {
	return isNTFSDotGit(seg) || isHFSDotGit(seg)
}

// isNTFSDotGit matches .git and its 8.3 short name git~1, in any case. NTFS
// drops trailing dots and spaces, and a colon starts a stream name.
func isNTFSDotGit(seg string) bool {
	var rest string
	switch {
	case len(seg) >= 4 && strings.EqualFold(seg[:4], ".git"):
		rest = seg[4:]
	case len(seg) >= 5 && strings.EqualFold(seg[:5], "git~1"):
		rest = seg[5:]
	default:
		return false
	}
	rest, _, _ = strings.Cut(rest, ":")
	return strings.Trim(rest, ". ") == ""
}

// hfsIgnored holds the code points HFS+ skips when it compares names, so .git
// with a zero-width joiner inside still opens the .git directory.
var hfsIgnored = map[rune]bool{
	0x200c: true, 0x200d: true, 0x200e: true, 0x200f: true,
	0x202a: true, 0x202b: true, 0x202c: true, 0x202d: true, 0x202e: true,
	0x206a: true, 0x206b: true, 0x206c: true, 0x206d: true, 0x206e: true, 0x206f: true,
	0xfeff: true,
}

func isHFSDotGit(seg string) bool {
	visible := strings.Map(func(r rune) rune {
		if hfsIgnored[r] {
			return -1
		}
		return r
	}, seg)
	return strings.EqualFold(visible, ".git")
}
