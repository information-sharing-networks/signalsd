package auth

//todo update

import (
	"fmt"
	"net/http"
	"testing"

	signalsd "github.com/nickabs/signalsd/app"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/rs/zerolog/log"
)

func TestHashPassword(t *testing.T) {
	cfg := signalsd.InitConfig(log.Logger)
	var queries *database.Queries
	authService := NewAuthService(cfg.SecretKey, cfg.Environment, queries)
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

func TestGetAccessToken(t *testing.T) {

	cfg := signalsd.InitConfig(log.Logger)
	var queries *database.Queries
	authService := NewAuthService(cfg.SecretKey, cfg.Environment, queries)
	accessToken := "token123"

	tests := []struct {
		name                string
		authorizationHeader string
		wantErr             bool
	}{
		{
			name:                "Valid header",
			authorizationHeader: fmt.Sprintf("Bearer %s", accessToken),
			wantErr:             false,
		},
		{
			name:                "Valid header with extra whitespace",
			authorizationHeader: fmt.Sprintf("Bearer  	%s 	", accessToken),
			wantErr:             false,
		},
		{
			name:                "Incorrect scheme",
			authorizationHeader: fmt.Sprintf("WrongScheme: %s", accessToken),
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
			token, err := authService.GetAccessTokenFromHeader(headers)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetAccessToken() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && token != accessToken {
				t.Errorf(`GetAccessToken() wrong token want "%v", got "%v"`, accessToken, token)
			}
		})
	}

}

/* todo
func TestCreateRefreshToken(t *testing.T) {
	cfg := signalsd.InitConfig()
	queries *database.Queries
	authService := NewAuthService(cfg.SecretKey, cfg.Environment, queries)
	token, err := authService.GenerateRefreshToken(context.Background(), queries)

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
*/
