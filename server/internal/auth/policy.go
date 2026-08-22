package auth

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	MinPasswordLength = 12
	MaxPasswordLength = 256
)

// commonPasswords are rejected outright. Length and character-class rules do not stop
// "Password123!", which is exactly what an attacker tries first.
var commonPasswords = map[string]struct{}{
	"password": {}, "password1": {}, "password123": {}, "password123!": {},
	"qwerty": {}, "qwerty123": {}, "123456789": {}, "1234567890": {},
	"letmein": {}, "welcome123": {}, "admin123": {}, "administrator": {},
	"changeme": {}, "changeme123": {}, "iloveyou": {}, "monkey123": {},
	"kubernetes": {}, "kubernetes1": {}, "dragon123": {}, "sunshine1": {},
	"abc123456": {}, "passw0rd": {}, "p@ssw0rd": {}, "p@ssword123": {},
	"trustno1": {}, "football123": {}, "baseball123": {}, "master123": {},
}

// PasswordProblems returns every policy violation at once so the user can fix them in
// one pass instead of discovering them one at a time.
func PasswordProblems(password, email, displayName string) []string {
	var problems []string

	runes := []rune(password)
	if len(runes) < MinPasswordLength {
		problems = append(problems, fmt.Sprintf("must be at least %d characters", MinPasswordLength))
	}
	if len(runes) > MaxPasswordLength {
		problems = append(problems, fmt.Sprintf("must be at most %d characters", MaxPasswordLength))
	}

	var hasUpper, hasLower, hasDigit, hasSymbol bool
	for _, r := range runes {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSymbol = true
		}
	}
	classes := 0
	for _, ok := range []bool{hasUpper, hasLower, hasDigit, hasSymbol} {
		if ok {
			classes++
		}
	}
	if classes < 3 {
		problems = append(problems, "must combine at least three of: uppercase, lowercase, digits, symbols")
	}

	lower := strings.ToLower(password)
	if _, found := commonPasswords[lower]; found {
		problems = append(problems, "is a commonly used password")
	}
	if isRepeatedOrSequential(lower) {
		problems = append(problems, "must not be a repeated character or a simple sequence")
	}

	if local, _, found := strings.Cut(strings.ToLower(email), "@"); found && len(local) >= 3 {
		if strings.Contains(lower, local) {
			problems = append(problems, "must not contain your email address")
		}
	}
	if name := strings.ToLower(strings.TrimSpace(displayName)); len(name) >= 3 && strings.Contains(lower, name) {
		problems = append(problems, "must not contain your name")
	}
	if strings.Contains(lower, "kubby") {
		problems = append(problems, "must not contain the application name")
	}

	return problems
}

// ValidatePassword reports the policy violations as a single error.
func ValidatePassword(password, email, displayName string) error {
	problems := PasswordProblems(password, email, displayName)
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%w: password %s", ErrWeakPassword, strings.Join(problems, "; "))
}

func isRepeatedOrSequential(s string) bool {
	if len(s) < 4 {
		return false
	}

	allSame := true
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] {
			allSame = false
			break
		}
	}
	if allSame {
		return true
	}

	ascending, descending := true, true
	for i := 1; i < len(s); i++ {
		if s[i] != s[i-1]+1 {
			ascending = false
		}
		if s[i] != s[i-1]-1 {
			descending = false
		}
	}
	return ascending || descending
}
