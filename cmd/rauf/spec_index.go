package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func listSpecs() ([]string, error) {
	entries := []string{}
	dir := "specs"
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, item.Name())
		status := readSpecStatus(path)
		if status == "" {
			status = "unknown"
		}
		entries = append(entries, path+" (status: "+status+")")
	}
	return entries, nil
}

func readSpecStatus(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	inFrontmatter := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			break
		}
		if !inFrontmatter {
			continue
		}
		if strings.HasPrefix(line, "status:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "status:"))
		}
	}
	return ""
}
