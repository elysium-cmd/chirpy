package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"net/http"
	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func HashPassword(password string) (string, error) {
	hash, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		return "", err
	}
	return hash, err
}

func CheckPasswordHash(password, hash string) (bool, error) {
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return false, err
	}
	return match, err
}

func MakeJWT(userID uuid.UUID, tokenSecret string) (string, error) {
	mySigningKey := []byte(tokenSecret)
	jwtStartTime := jwt.NewNumericDate(time.Now())
	jwtDuration := time.Duration(3600 * int(time.Second))
	jwtEndTime := jwt.NewNumericDate(jwtStartTime.Add(jwtDuration))

	claims := &jwt.RegisteredClaims{
		Issuer: "chirpy-access",
		IssuedAt: jwtStartTime,
		ExpiresAt: jwtEndTime,
		Subject: userID.String(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	ss, err := token.SignedString(mySigningKey)
	if err != nil {
		return "", err
	}
	return ss, err
}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(token *jwt.Token) (any, error) {
		return []byte(tokenSecret), nil
	}, jwt.WithLeeway(5 * time.Second))
	if err != nil {
		return uuid.Nil, err
	} else if claims, ok := token.Claims.(*jwt.RegisteredClaims); ok {
		return uuid.MustParse(claims.Subject), nil
	} else {
		return uuid.Nil, errors.New("Unknown claims type")
	}
}

func GetBearerToken(headers http.Header) (string, error) {
	bearerToken := headers.Get("Authorization")

	if bearerToken == "" {
		return "", errors.New("No bearer token found")
	} else {
		return strings.TrimPrefix(bearerToken, "Bearer "), nil
	}
}

func GetAPIKey(headers http.Header) (string, error) {
	apiKey := headers.Get("Authorization")

	if apiKey == "" {
		return "", errors.New("No api key found")
	} else {
		return strings.TrimPrefix(apiKey, "ApiKey "), nil
	}
}

func MakeRefreshToken() string {
	key := make([]byte, 32)
	rand.Read(key)
	return hex.EncodeToString(key)
}
