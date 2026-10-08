package fsm

const (
	zeroWidthJoiner   = '‍'
	variationSelector = '️'
	keycapMark        = '⃣'
	skinToneFirst     = 0x1f3fb
	skinToneLast      = 0x1f3ff
	regionalFirst     = 0x1f1e6
	regionalLast      = 0x1f1ff
	blackFlag         = 0x1f3f4
	tagFirst          = 0xe0020
	tagLast           = 0xe007e
	tagCancel         = 0xe007f
)

// isEmoji reports whether text is exactly one emoji: a pictograph with its
// variation selector or skin tone, a keycap, a flag, or pictographs joined by
// zero-width joiners. The pictograph test is by Unicode block, so a few symbols
// that are not emoji pass. Text, punctuation and several emoji in a row do not.
func isEmoji(text string) bool {
	runes := []rune(text)

	return isKeycap(runes) || isRegionalFlag(runes) || isJoinedSequence(runes)
}

func isKeycap(runes []rune) bool {
	if len(runes) == 0 || (runes[0] != '#' && runes[0] != '*' && (runes[0] < '0' || runes[0] > '9')) {
		return false
	}

	tail := runes[1:]
	if len(tail) > 0 && tail[0] == variationSelector {
		tail = tail[1:]
	}

	return len(tail) == 1 && tail[0] == keycapMark
}

func isRegionalFlag(runes []rune) bool {
	return len(runes) == 2 && isRegionalIndicator(runes[0]) && isRegionalIndicator(runes[1])
}

func isRegionalIndicator(r rune) bool { return r >= regionalFirst && r <= regionalLast }

func isJoinedSequence(runes []rune) bool {
	idx := 0

	for {
		next, ok := pictographElement(runes, idx)
		if !ok {
			return false
		}

		idx = next
		if idx == len(runes) {
			return true
		}

		if runes[idx] != zeroWidthJoiner {
			return false
		}

		idx++
	}
}

// pictographElement reads one pictograph at idx with its modifiers and returns
// the index after it.
func pictographElement(runes []rune, idx int) (next int, ok bool) {
	if idx >= len(runes) || !isPictograph(runes[idx]) {
		return idx, false
	}

	isFlag := runes[idx] == blackFlag

	idx = skipRune(runes, idx+1, variationSelector, variationSelector)
	idx = skipRune(runes, idx, skinToneFirst, skinToneLast)

	if isFlag {
		return subdivisionTags(runes, idx)
	}

	return idx, true
}

// subdivisionTags reads the tag letters and cancel tag that follow the black
// flag in a subdivision flag. The black flag alone is the pirate flag.
func subdivisionTags(runes []rune, idx int) (next int, ok bool) {
	tagged := idx
	for tagged < len(runes) && skipRune(runes, tagged, tagFirst, tagLast) != tagged {
		tagged++
	}

	if tagged == idx {
		return idx, true
	}

	end := skipRune(runes, tagged, tagCancel, tagCancel)

	return end, end != tagged
}

// skipRune returns idx past the rune at idx when it is within first and last,
// and idx otherwise.
func skipRune(runes []rune, idx int, first, last rune) int {
	if idx < len(runes) && runes[idx] >= first && runes[idx] <= last {
		return idx + 1
	}

	return idx
}

func isPictograph(char rune) bool {
	for _, block := range [][2]rune{
		{0x00a9, 0x00a9},
		{0x00ae, 0x00ae},
		{0x203c, 0x203c},
		{0x2049, 0x2049},
		{0x2122, 0x2122},
		{0x2139, 0x2139},
		{0x2194, 0x2199},
		{0x21a9, 0x21aa},
		{0x231a, 0x231b},
		{0x2328, 0x2328},
		{0x23cf, 0x23cf},
		{0x23e9, 0x23f3},
		{0x23f8, 0x23fa},
		{0x24c2, 0x24c2},
		{0x25aa, 0x25ab},
		{0x25b6, 0x25b6},
		{0x25c0, 0x25c0},
		{0x25fb, 0x25fe},
		{0x2600, 0x27bf},
		{0x2934, 0x2935},
		{0x2b05, 0x2b07},
		{0x2b1b, 0x2b1c},
		{0x2b50, 0x2b50},
		{0x2b55, 0x2b55},
		{0x3030, 0x3030},
		{0x303d, 0x303d},
		{0x3297, 0x3297},
		{0x3299, 0x3299},
		{0x1f000, 0x1faff},
	} {
		if char >= block[0] && char <= block[1] {
			return !isRegionalIndicator(char) && (char < skinToneFirst || char > skinToneLast)
		}
	}

	return false
}
