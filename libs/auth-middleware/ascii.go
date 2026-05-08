package authmw

// asciiToLower is the ASCII-only lowercaser, mirroring Rust's
// `str::to_ascii_lowercase`. Bytes outside the A-Z range pass
// through unchanged, so non-ASCII input round-trips byte-for-byte
// (where Go's `strings.ToLower` would Unicode-fold).
//
// Used by [CallerClearances] and [StaticMarkingNameResolver] so
// the Go port matches the Rust source's behaviour for marking
// names typed by humans.
func asciiToLower(s string) string {
	hasUpper := false
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			hasUpper = true
			break
		}
	}
	if !hasUpper {
		return s
	}
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		out[i] = b
	}
	return string(out)
}

// asciiEqualFold is the ASCII-only case-insensitive comparison,
// mirroring Rust's `str::eq_ignore_ascii_case`. Like
// [asciiToLower] this is byte-wise and never folds non-ASCII
// codepoints (Go's `strings.EqualFold` does, which would diverge
// from the Rust source).
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
