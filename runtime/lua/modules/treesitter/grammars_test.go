package treesitter

import (
	"reflect"
	"testing"
)

func TestGetLanguageInfo(t *testing.T) {
	tests := []struct {
		name      string
		alias     string
		want      *LanguageInfo // Now a pointer, but we compare values
		wantFound bool
	}{
		{
			name:      "Get existing language (primary alias)",
			alias:     "go",
			want:      copyLangInfo(supportedLanguages["go"]), // Create a copy
			wantFound: true,
		},
		{
			name:      "Get existing language (alternative alias)",
			alias:     "js",
			want:      copyLangInfo(supportedLanguages["js"]), // Create a copy
			wantFound: true,
		},
		{
			name:      "Get non-existing language",
			alias:     "xyz",
			want:      nil,
			wantFound: false,
		},
		{
			name:      "Get language with nil Language function",
			alias:     "markdown",
			want:      copyLangInfo(supportedLanguages["markdown"]), // Create a copy
			wantFound: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetLanguageInfo(tt.alias)
			if (got != nil) != tt.wantFound {
				t.Errorf("GetLanguageInfo() found = %v, wantFound %v", got != nil, tt.wantFound)
				return // Stop the test case here if found state is incorrect
			}

			// If not found, we don't need to compare values
			if !tt.wantFound {
				return
			}

			// Compare field by field, skipping the Language function
			if got.Name != tt.want.Name {
				t.Errorf("GetLanguageInfo() Name = %v, want %v", got.Name, tt.want.Name)
			}
			if !reflect.DeepEqual(got.Aliases, tt.want.Aliases) {
				t.Errorf("GetLanguageInfo() Aliases = %v, want %v", got.Aliases, tt.want.Aliases)
			}
			if got.GrammarContent != tt.want.GrammarContent {
				t.Errorf("GetLanguageInfo() GrammarContent = %v, want %v", got.GrammarContent, tt.want.GrammarContent)
			}
			// You can add a check for the Language function being nil or non-nil if needed,
			// but comparing function equality is generally not reliable.
			if (got.Language == nil) != (tt.want.Language == nil) {
				t.Errorf("GetLanguageInfo() Language presence = %v, want %v", got.Language == nil, tt.want.Language == nil)
			}
		})
	}
}

func TestGetSupportedLanguages(t *testing.T) {
	want := []string{
		"PHP", "Go", "JavaScript", "TypeScript with JSX", "TypeScript", "Python", "C#", "HTML", "Markdown",
	}
	got := GetSupportedLanguages()

	wantMap := make(map[string]bool, len(want))
	for _, lang := range want {
		wantMap[lang] = true
	}

	gotMap := make(map[string]bool, len(got))
	for _, lang := range got {
		gotMap[lang] = true
	}

	if !reflect.DeepEqual(wantMap, gotMap) {
		t.Errorf("GetSupportedLanguages() = %v, want %v", got, want)
	}
}

// TestLanguageFunctions checks if the Language functions return non-nil values.
func TestLanguageFunctions(t *testing.T) {
	for alias, info := range supportedLanguages {
		// Skip languages without a Language function (like Markdown)
		if info.Language == nil {
			continue
		}

		t.Run(alias, func(t *testing.T) {
			if got := info.Language(); got == nil {
				t.Errorf("Language() for '%s' returned nil, want non-nil", alias)
			}
		})
	}
}

// copyLangInfo creates a copy of a LanguageInfo value.
func copyLangInfo(info LanguageInfo) *LanguageInfo {
	// Create a new LanguageInfo with copies of the values
	newInfo := &LanguageInfo{
		Name:           info.Name,
		Aliases:        make([]string, len(info.Aliases)), // Copy the slice
		GrammarContent: info.GrammarContent,
		Language:       info.Language,
	}
	copy(newInfo.Aliases, info.Aliases) // Copy the slice contents
	return newInfo
}
