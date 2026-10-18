package auth

import "golang.org/x/crypto/bcrypt"

// HashPassword hashes at cost 10 and rejects passwords longer than 72 bytes.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword rejects overlong passwords and invalid hashes without truncation.
func CheckPassword(hash, password string) bool {
	if len(password) > 72 {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
