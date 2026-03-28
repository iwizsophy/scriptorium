package docs

import "github.com/silvekt/scriptorium/internal/filesafe"

func ScanFilesystem(root filesafe.Root, fallbackEncodings []string) ([]File, error) {
	paths, err := root.ListFiles([]string{".md"})
	if err != nil {
		return nil, err
	}

	files := make([]File, 0, len(paths))
	for _, path := range paths {
		textFile, err := root.ReadText(path, fallbackEncodings)
		if err != nil {
			return nil, err
		}
		files = append(files, ParseMarkdownFile(textFile.Path, textFile.Text))
	}

	return files, nil
}
