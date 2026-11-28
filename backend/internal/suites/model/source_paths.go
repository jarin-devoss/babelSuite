package model

import "strings"

// ExplicitSourceContent returns the content of the named file of a package.
func ExplicitSourceContent(files []SourceFile, path string) (string, bool) {
	for _, file := range files {
		if strings.TrimSpace(file.Path) == strings.TrimSpace(path) {
			return file.Content, true
		}
	}
	return "", false
}

// NormalizeSourcePath joins a folder and a file into a package-relative path.
func NormalizeSourcePath(folderName, file string) string {
	return strings.Trim(strings.TrimSpace(folderName)+"/"+strings.Trim(strings.TrimSpace(file), "/"), "/")
}
