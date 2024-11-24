package twemoji

import (
	"slices"
	"testing"
)

func TestFindEmojis(t *testing.T) {
	expectedEmojis := []string{"🌂", "🇫🇷", "🎀", "🐾", "⚓", "🫠", "👩🏼‍⚕️", "📘", "💧", "🍒", "✨", "🧪", "🐻", "💎", "🍫", "💘", "🤔"}
	testString := "🌂🇫🇷This 🎀🐾⚓🫠 is👩🏼‍⚕️ 📘💧🍒✨🧪🐻💎🍫💘 a test 🤔"

	locations := FindEmojis([]rune(testString))

	for _, info := range locations {
		t.Logf("Emoji: %s, Offset: %d, Length: %d", string(info.Runes), info.Offset, info.Length)
	}

	if len(locations) != len(expectedEmojis) {
		t.Errorf("expected %d emojis, found %d", len(expectedEmojis), len(locations))
	}

	for _, emoji := range expectedEmojis {
		contains := slices.ContainsFunc(locations, func(info EmojiText) bool {
			return slices.Equal(info.Runes, []rune(emoji))
		})
		if !contains {
			t.Errorf("expected emoji %s (%v) not found", emoji, []rune(emoji))
		}
	}
}

func TestEmojiToUnicodeHex(t *testing.T) {
	tests := []struct {
		emoji    string
		expected string
	}{
		{"🌂", "1f302"},
		{"🎀", "1f380"},
		{"🇫🇷", "1f1eb-1f1f7"},
	}

	for _, test := range tests {
		actual := emojiToHex([]rune(test.emoji))
		if actual != test.expected {
			t.Errorf("expected %s, got %s", test.expected, actual)
		}
	}
}
