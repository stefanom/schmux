package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: scan-ansi <log-file>")
		os.Exit(1)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	// Count sequence types and sample a few
	counts := make(map[byte]int)
	totalSeq := 0
	last100 := []string{}

	// Track unique sequences (last seen wins)
	uniqueSeqs := make(map[string][]byte)
	skipList := []byte{'H', 'f', 'A', 'B', 'C', 'D', 'J', 'K', 's', 'u', 'E', 'G', 'L', 'M', 'P', 'Z', '@', '`'}

	i := 0

	for i < len(data)-1 {
		if data[i] == 033 && i+1 < len(data) && data[i+1] == '[' {
			// Found CSI sequence
			j := i + 2
			for j < len(data) {
				if data[j] >= 0x40 && data[j] <= 0x7E {
					terminator := data[j]
					counts[terminator]++
					totalSeq++

					// Keep last 100 sequence samples
					seq := data[i : j+1]
					if len(last100) >= 100 {
						last100 = last100[1:]
					}
					last100 = append(last100, string(seq))

					// Track unique (last seen wins), skip cursor movements
					if !contains(skipList, terminator) {
						uniqueSeqs[string(seq)] = seq
					}
					break
				}
				j++
			}
			i = j + 1
		} else {
			i++
		}
	}

	// Calculate unique sequence size
	var uniqueSize int
	for _, seq := range uniqueSeqs {
		uniqueSize += len(seq)
	}

	fmt.Printf("Total CSI sequences: %d\n", totalSeq)
	fmt.Printf("Unique CSI sequences (after dedupe): %d\n", len(uniqueSeqs))
	fmt.Printf("File size: %.2f MB\n", float64(len(data))/(1024*1024))
	fmt.Printf("Unique sequence data size: %.2f MB\n", float64(uniqueSize)/(1024*1024))
	fmt.Println("\nSequence terminator counts:")
	for t, n := range counts {
		fmt.Printf("  %c (0x%02x): %d\n", t, t, n)
	}

	fmt.Println("\nLast 100 sequences (raw):")
	for _, seq := range last100 {
		fmt.Printf("%q\n", seq)
	}
}

func contains(slice []byte, b byte) bool {
	for _, v := range slice {
		if v == b {
			return true
		}
	}
	return false
}
