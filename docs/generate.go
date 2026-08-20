//go:generate go run generate.go yaml ../provider/internal/embed

// Package main turns each multi-document YAML example into one registry markdown page. It adapts
// the Apache-2.0 generator at https://github.com/pulumi/pulumi-docker-build/blob/main/docs/generate.go.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// colourCodes matches the terminal sequences the Pulumi CLI writes around its diagnostics.
var colourCodes = regexp.MustCompile("\x1b\\[[0-9;]*m")

// language names one converted program: the target of pulumi convert and the file it writes.
type language struct {
	name string
	file string
}

// languages is the whole set of SDK languages, in the order the markdown carries them.
func languages() []language {
	return []language{
		{"typescript", "index.ts"},
		{"python", "__main__.py"},
		{"csharp", "Program.cs"},
		{"go", "main.go"},
	}
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <example directory> <markdown directory>\n", os.Args[0])
		os.Exit(1)
	}
	if err := generate(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(exampleDir, markdownDir string) error {
	//nolint:gosec // G703: the directories are build-time arguments of the generator, not user input.
	if err := os.MkdirAll(markdownDir, 0o750); err != nil {
		return err
	}
	entries, err := os.ReadDir(exampleDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		source := filepath.Join(exampleDir, entry.Name())
		if err := writePage(source, markdownDir); err != nil {
			return fmt.Errorf("%s: %w", source, err)
		}
	}
	return nil
}

// writePage converts every example of one source file and writes the page the resource embeds.
func writePage(source, markdownDir string) error {
	documents, err := readDocuments(source)
	if err != nil {
		return err
	}
	if len(documents) == 0 {
		return errors.New("the file holds no example")
	}

	examples := make([]string, 0, len(documents))
	for i, document := range documents {
		example, err := convertDocument(document)
		if err != nil {
			return fmt.Errorf("example %d: %w", i, err)
		}
		examples = append(examples, example)
	}

	base := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source)) + ".md"
	page := filepath.Join(markdownDir, base)
	fmt.Println("writing", page)
	//nolint:gosec // G703: the page sits under the destination the generate directive names.
	return os.WriteFile(page, []byte(wrapExamples(examples)), 0o600)
}

func readDocuments(source string) ([]map[string]any, error) {
	file, err := os.Open(filepath.Clean(source))
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	decoder := yaml.NewDecoder(file)
	var documents []map[string]any
	for {
		document := map[string]any{}
		if err := decoder.Decode(&document); err != nil {
			if errors.Is(err, io.EOF) {
				return documents, nil
			}
			return nil, err
		}
		documents = append(documents, document)
	}
}

// convertDocument writes one example as a Pulumi program, then converts it to every language.
func convertDocument(document map[string]any) (string, error) {
	description, _ := document["description"].(string)
	if description == "" {
		return "", errors.New("the example needs a description, which becomes its heading")
	}

	dir, err := os.MkdirTemp("", "mailslurp-example")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	source, err := yaml.Marshal(document)
	if err != nil {
		return "", err
	}
	//nolint:gosec // G703: the program sits in the temporary directory this function just made.
	if err := os.WriteFile(filepath.Join(dir, "Pulumi.yaml"), source, 0o600); err != nil {
		return "", err
	}

	var blocks strings.Builder
	for _, target := range languages() {
		program, err := convert(dir, target)
		if err != nil {
			return "", fmt.Errorf("%s: %w", target.name, err)
		}
		fmt.Fprintf(&blocks, "```%s\n%s```\n", target.name, program)
	}
	return fmt.Sprintf("{{%% example %%}}\n### %s\n\n%s{{%% /example %%}}\n",
		description, blocks.String()), nil
}

// convert shells out to the Pulumi CLI. The CLI reports an unknown resource or property on the
// error stream and still exits with the code 0, so that stream decides whether the program is whole.
func convert(dir string, target language) (string, error) {
	out := filepath.Join(dir, "converted-"+target.name)
	//nolint:gosec // G204: the arguments are the build's own constants and its temporary directory.
	command := exec.Command("pulumi", "convert",
		"--language", target.name, "--out", out, "--generate-only")
	command.Dir = dir

	var captured strings.Builder
	command.Stderr = &captured
	_, err := command.Output()
	// The converter colours its own diagnostics whatever --color says, so the codes come out
	// before the match: the marker on the wire is "Error" wrapped in a reset sequence.
	diagnostics := colourCodes.ReplaceAllString(captured.String(), "")
	fmt.Fprint(os.Stderr, diagnostics)
	if err != nil {
		return "", err
	}
	if strings.Contains(diagnostics, "Error:") {
		return "", errors.New("the conversion reported an error, printed above")
	}

	program, err := os.ReadFile(filepath.Clean(filepath.Join(out, target.file)))
	if err != nil {
		return "", err
	}
	text := string(program)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text, nil
}

// wrapExamples adds the registry shortcodes. The trailing newline keeps the last shortcode apart
// from the next description when the prose check reads every schema description as one stream.
func wrapExamples(examples []string) string {
	return "{{% examples %}}\n## Example Usage\n" + strings.Join(examples, "") + "{{% /examples %}}\n"
}
