package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

// Integration tests for Cabinet Service
// Requires running PostgreSQL and Cabinet service

var (
	cabinetURL    string
	databaseURL   string
	testPool      *pgxpool.Pool
	testUserEmail string = "test_integration@example.com"
	testPassword  string = "test_password"
)

func TestMain(m *testing.M) {
	// Load .env if exists
	_ = godotenv.Load("../../.env")

	cabinetURL = getEnv("CABINET_URL", "http://localhost:8084")
	databaseURL = getEnv("DATABASE_URL", "postgres://api_scan:api_scan_secret@localhost:5432/api_scan")

	// Setup
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		fmt.Printf("SKIP: Failed to connect to database: %v\n", err)
		os.Exit(0)
	}
	testPool = pool

	// Health check cabinet service
	if _, err := http.Get(cabinetURL + "/health"); err != nil {
		fmt.Printf("SKIP: Cabinet service not available at %s: %v\n", cabinetURL, err)
		testPool.Close()
		os.Exit(0)
	}

	// Wait for services
	time.Sleep(2 * time.Second)

	// Run tests
	code := m.Run()

	// Cleanup
	cleanupTestData(ctx)
	testPool.Close()

	os.Exit(code)
}

func cleanupTestData(ctx context.Context) {
	// Remove test user and org
	_, _ = testPool.Exec(ctx, "DELETE FROM account_events WHERE org_id IN (SELECT id FROM organizations WHERE email = $1)", testUserEmail)
	_, _ = testPool.Exec(ctx, "DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE email = $1)", testUserEmail)
	_, _ = testPool.Exec(ctx, "DELETE FROM users WHERE email = $1", testUserEmail)
	_, _ = testPool.Exec(ctx, "DELETE FROM organizations WHERE email = $1", testUserEmail)
}

func TestAuthFlow(t *testing.T) {
	ctx := context.Background()

	t.Run("Register and Login", func(t *testing.T) {
		// Cleanup first
		cleanupTestData(ctx)

		// 1. Register
		registerBody := map[string]string{
			"organization_name": "Test Integration Org",
			"email":             testUserEmail,
			"password":          testPassword,
		}
		body, _ := json.Marshal(registerBody)

		resp, err := http.Post(cabinetURL+"/api/v1/auth/register", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Register request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("Expected 201, got %d", resp.StatusCode)
		}

		var registerResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&registerResp)
		if registerResp["org_id"] == nil {
			t.Fatal("Expected org_id in response")
		}

		// Activate user (normally done via email verification)
		_, err = testPool.Exec(ctx, 
			"UPDATE organizations SET status = 'active', email_verified = true WHERE email = $1", 
			testUserEmail)
		if err != nil {
			t.Fatalf("Failed to activate user: %v", err)
		}

		// 2. Login
		loginBody := map[string]string{
			"email":    testUserEmail,
			"password": testPassword,
		}
		body, _ = json.Marshal(loginBody)

		resp, err = http.Post(cabinetURL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Login request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var loginResp struct {
			SessionToken string `json:"session_token"`
			User         struct {
				ID    int64  `json:"id"`
				Email string `json:"email"`
				Role  string `json:"role"`
			} `json:"user"`
		}
		json.NewDecoder(resp.Body).Decode(&loginResp)

		if loginResp.SessionToken == "" {
			t.Fatal("Expected session_token in response")
		}
		if loginResp.User.Email != testUserEmail {
			t.Fatalf("Expected email %s, got %s", testUserEmail, loginResp.User.Email)
		}

		// 3. Verify session
		req, _ := http.NewRequest("GET", cabinetURL+"/api/v1/auth/verify", nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.SessionToken)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err = client.Do(req)
		if err != nil {
			t.Fatalf("Verify request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 from verify, got %d", resp.StatusCode)
		}

		// 4. Logout
		req, _ = http.NewRequest("POST", cabinetURL+"/api/v1/auth/logout", nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.SessionToken)

		resp, err = client.Do(req)
		if err != nil {
			t.Fatalf("Logout request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 from logout, got %d", resp.StatusCode)
		}

		// 5. Try to use revoked session
		req, _ = http.NewRequest("GET", cabinetURL+"/api/v1/auth/verify", nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.SessionToken)

		resp, err = client.Do(req)
		if err != nil {
			t.Fatalf("Verify after logout failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("Expected 401 after logout, got %d", resp.StatusCode)
		}
	})

	t.Run("API Key Management", func(t *testing.T) {
		// Use a different email to avoid conflicts
		testEmail2 := "test_integration2@example.com"
		
		// Cleanup
		_, _ = testPool.Exec(ctx, "DELETE FROM account_events WHERE org_id IN (SELECT id FROM organizations WHERE email = $1)", testEmail2)
		_, _ = testPool.Exec(ctx, "DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE email = $1)", testEmail2)
		_, _ = testPool.Exec(ctx, "DELETE FROM users WHERE email = $1", testEmail2)
		_, _ = testPool.Exec(ctx, "DELETE FROM organizations WHERE email = $1", testEmail2)

		// Create and activate user with password 'password' (same as testPassword constant)
		// Using the same hash as in seed.sql
		hash := "$2a$10$kIxY6tX2MRiV4tROQZHKOenezw37Hdc1s14qDCSy9jsqBYFDP2Xde"
		
		_, err := testPool.Exec(ctx, 
			`INSERT INTO organizations (name, email, email_verified, password_hash, status, billing_account_id)
			 VALUES ('Test Org', $1, true, $2, 'active', 1)`,
			testEmail2, hash)
		if err != nil {
			t.Fatalf("Failed to create org: %v", err)
		}
		
		var orgID int64
		testPool.QueryRow(ctx, "SELECT id FROM organizations WHERE email = $1", testEmail2).Scan(&orgID)
		
		_, err = testPool.Exec(ctx, 
			`INSERT INTO users (org_id, email, password_hash, role)
			 VALUES ($1, $2, $3, 'admin')`,
			orgID, testEmail2, hash)
		if err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}
		
		// Use 'password' for login in this test
		testPassword := "password"

		// Login with the new user
		loginBody := map[string]string{
			"email":    testEmail2,
			"password": testPassword,
		}
		body, _ := json.Marshal(loginBody)

		resp, err := http.Post(cabinetURL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Login failed: %v", err)
		}
		defer resp.Body.Close()

		var loginResp struct {
			SessionToken string `json:"session_token"`
		}
		json.NewDecoder(resp.Body).Decode(&loginResp)
		token := loginResp.SessionToken

		client := &http.Client{Timeout: 10 * time.Second}

		// 1. Create API key
		keyBody := map[string]string{"name": "Test Key"}
		body, _ = json.Marshal(keyBody)
		
		req, _ := http.NewRequest("POST", cabinetURL+"/api/v1/api-keys", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err = client.Do(req)
		if err != nil {
			t.Fatalf("Create key failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 201, got %d: %s", resp.StatusCode, string(bodyBytes))
		}

		var keyResp struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			FullKey string `json:"full_key"`
		}
		json.NewDecoder(resp.Body).Decode(&keyResp)

		if keyResp.FullKey == "" {
			t.Fatal("Expected full_key in response")
		}

		// 2. List API keys
		req, _ = http.NewRequest("GET", cabinetURL+"/api/v1/api-keys", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err = client.Do(req)
		if err != nil {
			t.Fatalf("List keys failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		var keys []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&keys)

		if len(keys) != 1 {
			t.Fatalf("Expected 1 key, got %d", len(keys))
		}

		// 3. Verify API key format is valid (can be parsed by API Gateway)
		// The key should be in format base64(key_id:secret)
		decoded, err := base64.StdEncoding.DecodeString(keyResp.FullKey)
		if err != nil {
			t.Fatalf("API Key is not valid base64: %v", err)
		}
		parts := bytes.SplitN(decoded, []byte(":"), 2)
		if len(parts) != 2 {
			t.Fatalf("API Key format invalid, expected key_id:secret")
		}
		t.Logf("API Key format valid: key_id=%s, secret_len=%d", string(parts[0]), len(parts[1]))
		
		// 4. List keys again to verify count
		req, _ = http.NewRequest("GET", cabinetURL+"/api/v1/api-keys", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err = client.Do(req)
		if err != nil {
			t.Fatalf("List keys failed: %v", err)
		}
		defer resp.Body.Close()

		var keys2 []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&keys2)

		if len(keys2) != 1 {
			t.Fatalf("Expected 1 key in list, got %d", len(keys2))
		}
		t.Logf("Key list verified: %d keys", len(keys2))
	})
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
