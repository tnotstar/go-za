package issue

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ScanSummaries reads the todo.txt first line of every issue record in dir,
// in file-name order. Only the first two lines of each file are read, so
// listing and board rendering never load the Markdown bodies. Entries that
// are not I-NNNNNN.md records (such as .gitkeep) are skipped; a matching
// file name whose line violates the grammar is an error, never a silent
// skip.
func ScanSummaries(dir string) ([]Issue, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}
	var out []Issue
	for _, de := range des {
		m := filePattern.FindStringSubmatch(de.Name())
		if m == nil {
			continue
		}
		if !de.Type().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", filepath.Join(dir, de.Name()))
		}
		iss, err := readHeader(filepath.Join(dir, de.Name()), m[1])
		if err != nil {
			return nil, err
		}
		out = append(out, iss)
	}
	return out, nil
}

// readHeader parses the first line of the record at path and validates the
// two invariants the file name cannot enforce by itself: the id: tag must
// match the file name, and line 2 must be blank (or the file ends there).
func readHeader(path, idDigits string) (Issue, error) {
	f, err := os.Open(path)
	if err != nil {
		return Issue{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	r := bufio.NewReader(f)
	line1, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return Issue{}, fmt.Errorf("read %s: %w", path, err)
	}
	line1 = strings.TrimSuffix(line1, "\n")
	if strings.HasSuffix(line1, "\r") {
		return Issue{}, fmt.Errorf("%s: the record line ends with a carriage return; records are LF-only", path)
	}
	iss, err := Parse(line1)
	if err != nil {
		return Issue{}, fmt.Errorf("%s: %w", path, err)
	}
	if iss.ID != FormatID(mustAtoi(idDigits)) {
		return Issue{}, fmt.Errorf("%s: the file name does not match the id tag %q", path, iss.ID)
	}
	line2, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return Issue{}, fmt.Errorf("read %s: %w", path, err)
	}
	if strings.HasSuffix(line2, "\r") && line2 != "\r" {
		return Issue{}, fmt.Errorf("%s: line 2 ends with a carriage return; records are LF-only", path)
	}
	if line2 = strings.TrimSuffix(strings.TrimSuffix(line2, "\n"), "\r"); line2 != "" {
		return Issue{}, fmt.Errorf("%s: line 2 must be blank", path)
	}
	return iss, nil
}

func mustAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic("issue: malformed six-digit ID digits " + s + ": " + err.Error())
	}
	return n
}

// NextID returns the next free sequential issue ID: I-000001 when the
// directory holds no records yet, otherwise the highest existing number
// plus one. It fails closed when the six-digit ID space is exhausted.
func NextID(dir string) (string, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("list %s: %w", dir, err)
	}
	max := 0
	for _, de := range des {
		m := filePattern.FindStringSubmatch(de.Name())
		if m == nil {
			continue
		}
		if n, err := strconv.Atoi(m[1]); err == nil && n > max {
			max = n
		}
	}
	if max >= 999999 {
		return "", fmt.Errorf("issue ID space exhausted at %s; archive records to free IDs", FormatID(max))
	}
	return FormatID(max + 1), nil
}
