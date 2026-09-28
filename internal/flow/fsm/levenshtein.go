package fsm

// levenshtein returns the number of single-character insertions, deletions and
// substitutions that turn a into b. It counts runes, so accented letters are one.
func levenshtein(a, b string) int {
	runesA, runesB := []rune(a), []rune(b)

	prev := make([]int, len(runesB)+1)
	for col := range prev {
		prev[col] = col
	}

	for row := 1; row <= len(runesA); row++ {
		cur := make([]int, len(runesB)+1)
		cur[0] = row

		for col := 1; col <= len(runesB); col++ {
			cost := 1
			if runesA[row-1] == runesB[col-1] {
				cost = 0
			}

			cur[col] = min(prev[col]+1, cur[col-1]+1, prev[col-1]+cost)
		}

		prev = cur
	}

	return prev[len(runesB)]
}
