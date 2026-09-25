package protocolupgrade

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

const commonRSRef = "codex-rs/app-server-protocol/src/protocol/common.rs"

type requestMapping struct {
	variant      string
	responseType string
	macroName    string
}

var requestEntryRE = regexp.MustCompile(`(?s)(?P<prefix>(?:\s*(?:#\[[^\n]*\]|///[^\n]*|//[^\n]*)\n)*)\s*(?P<variant>[A-Za-z][A-Za-z0-9_]*)(?:\s*=>\s*"(?P<wire>[^"]+)")?\s*\{(?P<body>.*?)\n\s*\},`)
var responseEntryRE = regexp.MustCompile(`response:\s*([^,\n]+)`)

func loadCommonRSSourceSHA(commonRS, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	sidecar := commonRS + ".source_sha"
	raw, err := os.ReadFile(sidecar)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func verifyCommonRSProvenance(commonRS, sourceSHA, targetSHA, codexRepo string) error {
	if sourceSHA == "" {
		return fmt.Errorf("common.rs source SHA is required")
	}
	if sourceSHA != targetSHA {
		return fmt.Errorf("common.rs source SHA %s does not match target %s", sourceSHA, targetSHA)
	}
	if codexRepo == "" {
		return nil
	}
	cmd := exec.Command("git", "-C", codexRepo, "show", targetSHA+":"+commonRSRef)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git show %s:%s: %w", targetSHA, commonRSRef, err)
	}
	got, err := os.ReadFile(commonRS)
	if err != nil {
		return err
	}
	if string(out) != string(got) {
		return fmt.Errorf("common.rs content does not match %s:%s", targetSHA, commonRSRef)
	}
	return nil
}

func parseRequestMappings(path string) (map[string]requestMapping, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	body := string(text)
	mappings := map[string]requestMapping{}
	for _, macroName := range []string{"client_request_definitions", "server_request_definitions"} {
		macroBody, err := extractMacroBody(body, macroName)
		if err != nil {
			return nil, err
		}
		for _, match := range requestEntryRE.FindAllStringSubmatch(macroBody, -1) {
			named := namedGroups(requestEntryRE, match)
			wire := named["wire"]
			if wire == "" {
				continue
			}
			response := responseEntryRE.FindStringSubmatch(named["body"])
			if response == nil {
				continue
			}
			if _, duplicate := mappings[wire]; duplicate {
				return nil, fmt.Errorf("duplicate response mapping for %q", wire)
			}
			mappings[wire] = requestMapping{
				variant:      named["variant"],
				responseType: responseTypeName(response[1]),
				macroName:    macroName,
			}
		}
	}
	return mappings, nil
}

func extractMacroBody(text, macroName string) (string, error) {
	marker := macroName + "!"
	start := strings.Index(text, marker)
	if start < 0 {
		return "", fmt.Errorf("macro %s not found", macroName)
	}
	open := strings.Index(text[start:], "{")
	if open < 0 {
		return "", fmt.Errorf("macro %s is missing a body", macroName)
	}
	openIndex := start + open
	closeIndex, err := findMatchingBrace(text, openIndex)
	if err != nil {
		return "", err
	}
	return text[openIndex+1 : closeIndex], nil
}

func findMatchingBrace(text string, openIndex int) (int, error) {
	depth := 0
	inString := false
	escape := false
	for index := openIndex; index < len(text); index++ {
		char := text[index]
		if inString {
			if escape {
				escape = false
			} else if char == '\\' {
				escape = true
			} else if char == '"' {
				inString = false
			}
			continue
		}
		if char == '"' {
			inString = true
			continue
		}
		if char == '{' {
			depth++
			continue
		}
		if char == '}' {
			depth--
			if depth == 0 {
				return index, nil
			}
		}
	}
	return 0, fmt.Errorf("unbalanced braces while parsing common.rs")
}

func responseTypeName(responseType string) string {
	value := strings.TrimSpace(responseType)
	value = strings.ReplaceAll(value, "crate::protocol::", "")
	value = strings.ReplaceAll(value, "crate::", "")
	value = strings.ReplaceAll(value, "super::", "")
	if idx := strings.LastIndex(value, "::"); idx >= 0 {
		value = value[idx+2:]
	}
	return value
}

func namedGroups(re *regexp.Regexp, match []string) map[string]string {
	out := map[string]string{}
	for i, name := range re.SubexpNames() {
		if i == 0 || name == "" || i >= len(match) {
			continue
		}
		out[name] = match[i]
	}
	return out
}
