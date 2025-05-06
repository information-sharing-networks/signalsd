package auth

//todo update

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nickabs/signals"
)

func TestHashPassword(t *testing.T) {
	cfg := signals.InitConfig()
	authService := NewAuthService(cfg)
	password1 := "testpassword"
	hash1, _ := authService.HashPassword(password1)

	tests := []struct {
		name     string
		password string
		hash     string
		wantErr  bool
	}{
		{
			name:     "Correct password",
			password: password1,
			hash:     hash1,
			wantErr:  false,
		},
		{
			name:     "Incorrect password",
			password: "wrongpassword",
			hash:     hash1,
			wantErr:  true,
		},
		{
			name:     "Empty password",
			password: "",
			hash:     hash1,
			wantErr:  true,
		},
		{
			name:     "Invalid hash",
			password: password1,
			hash:     "invalidhash",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			err := authService.CheckPasswordHash(tt.hash, tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckPasswordHash() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Make sure that you can create and validate JWTs, and that expired tokens are rejected and JWTs signed with the wrong secret are rejected.
func TestParseBearerToken(t *testing.T) {

	authService := NewAuthService(signals.InitConfig())
	const tokenSecret = "secret"
	testUUID, err := uuid.NewRandom()
	duration := 60 * time.Second

	if err != nil {
		t.Fatalf("failed to generate test UUID: %v", err)
	}

	validToken, err := authService.GenerateAccessToken(testUUID, tokenSecret, duration)
	if err != nil {
		t.Fatalf("failed to create token for test: %v", err)
	}
	expiredToken, err := authService.GenerateAccessToken(testUUID, tokenSecret, duration*-1)
	if err != nil {
		t.Fatalf("failed to create token for test: %v", err)
	}

	tests := []struct {
		name        string
		tokenString string
		tokenSecret string
		duration    time.Duration
		wantUUID    uuid.UUID
		wantErr     bool
	}{{
		name:        "Valid Token",
		tokenString: validToken,
		tokenSecret: tokenSecret,
		duration:    duration,
		wantUUID:    testUUID,
		wantErr:     false,
	}, {
		name:        "Wrong Secret",
		tokenString: validToken,
		tokenSecret: "wrongsecret",
		duration:    duration,
		wantUUID:    testUUID,
		wantErr:     true,
	}, {
		name:        "Expired Token",
		tokenString: expiredToken,
		tokenSecret: tokenSecret,
		duration:    duration,
		wantUUID:    testUUID,
		wantErr:     true,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := authService.ValidateJWT(tt.tokenString, tt.tokenSecret)
			if err != nil && !tt.wantErr {
				t.Errorf("ValidateAndParseBearerToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			rawID := claims.Subject
			userID, err := uuid.Parse(rawID)
			if err == nil && userID != tt.wantUUID {
				t.Errorf("ValidateAndParseBearerToken() gotUserID = %v, want %v", userID, tt.wantUUID)
			}
		})
	}
}

func TestGetBearerToken(t *testing.T) {
	authService := NewAuthService(signals.InitConfig())
	bearerToken := "token123"

	tests := []struct {
		name                string
		authorizationHeader string
		wantErr             bool
	}{
		{
			name:                "Valid header",
			authorizationHeader: fmt.Sprintf("Bearer %s", bearerToken),
			wantErr:             false,
		},
		{
			name:                "Valid header with extra whitespace",
			authorizationHeader: fmt.Sprintf("Bearer  	%s 	", bearerToken),
			wantErr:             false,
		},
		{
			name:                "Incorrect scheme",
			authorizationHeader: fmt.Sprintf("WrongScheme: %s", bearerToken),
			wantErr:             true,
		},
		{
			name:                "Missing header",
			authorizationHeader: "",
			wantErr:             true,
		},
		{
			name:                "Missing token",
			authorizationHeader: "Bearer",
			wantErr:             true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			if tt.authorizationHeader != "" {
				headers.Set("Authorization", tt.authorizationHeader)
			}
			token, err := authService.BearerTokenFromHeader(headers)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetBearerToken() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && token != bearerToken {
				t.Errorf(`GetBearerToken() wrong token want "%v", got "%v"`, bearerToken, token)
			}
		})
	}

}

func TestCreateRefreshToken(t *testing.T) {
	authService := NewAuthService(signals.InitConfig())
	token, err := authService.GenerateRefreshToken()

	if err != nil {
		t.Fatalf("Token generation failed: %v", err)
	}

	if len(token) != 64 {
		t.Errorf("Token has %d characters instead of expected 64", len(token))
	}

	_, err = hex.DecodeString(token)
	if err != nil {
		t.Errorf("Token is not valid hexadecimal: %v", err)
	}
}
