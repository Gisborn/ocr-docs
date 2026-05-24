package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

// E2E Billing Flow Test
// Requires: PostgreSQL, Cabinet Service, API Gateway, Billing Service

var (
	apiGatewayURL string
)

func init() {
	_ = godotenv.Load("../../.env")
	apiGatewayURL = getEnv("API_GATEWAY_URL", "http://localhost:8080")
}

func TestBillingFlow(t *testing.T) {
	ctx := context.Background()

	// Check dependencies
	if _, err := http.Get(cabinetURL + "/health"); err != nil {
		t.Skipf("Cabinet service not available: %v", err)
	}
	if _, err := http.Get(apiGatewayURL + "/health"); err != nil {
		t.Skipf("API Gateway not available: %v", err)
	}

	// Connect to DB
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Skipf("Database not available: %v", err)
	}
	defer pool.Close()

	testEmail := "billing_flow_test@example.com"
	cleanupBillingFlow(ctx, pool, testEmail)

	client := &http.Client{Timeout: 10 * time.Second}

	// ── 1. Register via Cabinet ──
	t.Run("Register", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"organization_name": "Billing Flow Test Org",
			"email":             testEmail,
			"password":          testPassword,
		})
		resp, err := http.Post(cabinetURL+"/api/v1/auth/register", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 201, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
		var r map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&r)
		if r["org_id"] == nil {
			t.Fatal("Expected org_id in register response")
		}
	})

	// Activate user (bypass email verification)
	_, _ = pool.Exec(ctx, "UPDATE organizations SET status = 'active', email_verified = true WHERE email = $1", testEmail)

	// ── 2. Login ──
	var sessionToken string
	t.Run("Login", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"email": testEmail, "password": testPassword})
		resp, err := http.Post(cabinetURL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Login failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
		var r struct{ SessionToken string `json:"session_token"` }
		json.NewDecoder(resp.Body).Decode(&r)
		sessionToken = r.SessionToken
	})

	// ── 3. Create API Key ──
	var apiKey string
	t.Run("Create API Key", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"name": "E2E Billing Key"})
		req, _ := http.NewRequest("POST", cabinetURL+"/api/v1/api-keys", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+sessionToken)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Create key failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 201, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
		var r struct{ FullKey string `json:"full_key"` }
		json.NewDecoder(resp.Body).Decode(&r)
		apiKey = r.FullKey
	})

	// Verify API key format
	decoded, err := base64.StdEncoding.DecodeString(apiKey)
	if err != nil {
		t.Fatalf("Invalid API key format: %v", err)
	}
	parts := bytes.SplitN(decoded, []byte(":"), 2)
	if len(parts) != 2 {
		t.Fatal("API key format invalid")
	}
	keyID := string(parts[0])
	t.Logf("API Key ID: %s", keyID)

	// Get billing_account_id for the org
	var billingAccountID int64
	err = pool.QueryRow(ctx, "SELECT billing_account_id FROM organizations WHERE email = $1", testEmail).Scan(&billingAccountID)
	if err != nil || billingAccountID == 0 {
		// Create billing account via API Gateway if not linked
		req, _ := http.NewRequest("POST", apiGatewayURL+"/v1/billing/accounts", nil)
		req.Header.Set("X-Api-Key", apiKey)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Create billing account failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 201, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
		var acc map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&acc)
		if id, ok := acc["id"].(float64); ok {
			billingAccountID = int64(id)
			_, _ = pool.Exec(ctx, "UPDATE organizations SET billing_account_id = $1 WHERE email = $2", billingAccountID, testEmail)
		}
	}

	// ── 4. Topup Balance ──
	t.Run("Topup Balance", func(t *testing.T) {
		body, _ := json.Marshal(map[string]float64{"amount_rub": 2000})
		req, _ := http.NewRequest("POST", fmt.Sprintf("%s/v1/billing/accounts/%d/topup", apiGatewayURL, billingAccountID), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", apiKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Topup failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
		var r map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&r)
		if r["status"] != "success" {
			t.Fatalf("Expected success, got %v", r["status"])
		}
	})

	// ── 5. Check Balance ──
	t.Run("Check Balance", func(t *testing.T) {
		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/v1/billing/accounts/%d/balance", apiGatewayURL, billingAccountID), nil)
		req.Header.Set("X-Api-Key", apiKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Balance check failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
		var r map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&r)
		realBalance, _ := r["real_balance_rub"].(float64)
		if realBalance < 1500 {
			t.Fatalf("Expected balance >= 1500 after topup, got %.2f", realBalance)
		}
		t.Logf("Balance: real=%.2f, prepaid=%.2f", realBalance, r["prepaid_balance_rub"])
	})

	// ── 6. Create Subscription (Pro) ──
	t.Run("Create Subscription", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"tariff_code":     "pro",
			"payment_method":  "balance",
		})
		req, _ := http.NewRequest("POST", fmt.Sprintf("%s/v1/billing/accounts/%d/subscriptions", apiGatewayURL, billingAccountID), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", apiKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Create subscription failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 201, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
		var r map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&r)
		if r["status"] != "active" {
			t.Fatalf("Expected active subscription, got %v", r["status"])
		}
		t.Logf("Subscription created: status=%s, amount_charged=%.2f", r["status"], r["amount_charged_rub"])
	})

	// ── 7. Reserve Funds ──
	t.Run("Reserve Funds", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"service_id": "passport_rf",
			"request_id": "e2e_req_001",
		})
		req, _ := http.NewRequest("POST", fmt.Sprintf("%s/v1/billing/accounts/%d/reserve", apiGatewayURL, billingAccountID), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", apiKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Reserve failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
		var r map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&r)
		if !r["reserved"].(bool) {
			t.Fatalf("Expected reserved=true, got %v", r["reserved"])
		}
		t.Logf("Reserved: charge_type=%s, amount=%.2f", r["charge_type"], r["amount_rub"])
	})

	// ── 8. Check Balance After Reserve ──
	t.Run("Balance After Reserve", func(t *testing.T) {
		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/v1/billing/accounts/%d/balance", apiGatewayURL, billingAccountID), nil)
		req.Header.Set("X-Api-Key", apiKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Balance check failed: %v", err)
		}
		defer resp.Body.Close()
		var r map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&r)
		t.Logf("Balance after reserve: real=%.2f, prepaid=%.2f", r["real_balance_rub"], r["prepaid_balance_rub"])
	})

	// ── 9. Commit Transaction ──
	t.Run("Commit Transaction", func(t *testing.T) {
		req, _ := http.NewRequest("POST", fmt.Sprintf("%s/v1/billing/transactions/e2e_req_001/commit", apiGatewayURL), nil)
		req.Header.Set("X-Api-Key", apiKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Commit failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
		var r map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&r)
		if r["status"] != "committed" {
			t.Fatalf("Expected committed, got %v", r["status"])
		}
	})

	// ── 10. Check Balance After Commit ──
	t.Run("Balance After Commit", func(t *testing.T) {
		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/v1/billing/accounts/%d/balance", apiGatewayURL, billingAccountID), nil)
		req.Header.Set("X-Api-Key", apiKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Balance check failed: %v", err)
		}
		defer resp.Body.Close()
		var r map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&r)
		realBalance, _ := r["real_balance_rub"].(float64)
		if realBalance >= 2000 {
			t.Fatalf("Expected balance decreased after commit, got %.2f", realBalance)
		}
		t.Logf("Balance after commit: real=%.2f, prepaid=%.2f", realBalance, r["prepaid_balance_rub"])
	})

	// ── 11. Get Billing Events ──
	t.Run("Billing Events History", func(t *testing.T) {
		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/v1/billing/accounts/%d/events", apiGatewayURL, billingAccountID), nil)
		req.Header.Set("X-Api-Key", apiKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Events request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
		var events []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&events)
		if len(events) == 0 {
			t.Fatal("Expected billing events, got none")
		}
		t.Logf("Billing events count: %d", len(events))
	})

	// ── 12. Duplicate Commit Should Fail ──
	t.Run("Duplicate Commit Fails", func(t *testing.T) {
		req, _ := http.NewRequest("POST", fmt.Sprintf("%s/v1/billing/transactions/e2e_req_001/commit", apiGatewayURL), nil)
		req.Header.Set("X-Api-Key", apiKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Second commit failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError && resp.StatusCode != http.StatusNotFound {
			t.Fatalf("Expected 500 or 404 on duplicate commit, got %d", resp.StatusCode)
		}
	})

	// Cleanup
	cleanupBillingFlow(ctx, pool, testEmail)
}

func cleanupBillingFlow(ctx context.Context, pool *pgxpool.Pool, email string) {
	_, _ = pool.Exec(ctx, "DELETE FROM account_events WHERE org_id IN (SELECT id FROM organizations WHERE email = $1)", email)
	_, _ = pool.Exec(ctx, "DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE email = $1)", email)
	_, _ = pool.Exec(ctx, "DELETE FROM api_keys WHERE org_id IN (SELECT id FROM organizations WHERE email = $1)", email)
	_, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email)
	_, _ = pool.Exec(ctx, "DELETE FROM organizations WHERE email = $1", email)
}
