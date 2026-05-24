package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"scan.passport.local/api/services/cabinet/pkg/models"
)

func setupAuthService() (*AuthService, *MockRepository, *httptest.Server) {
	repo := NewMockRepository()
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/accounts" {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]int64{"id": 999})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	authSvc := NewAuthService(repo, billingServer.URL, "test-token")
	return authSvc, repo, billingServer
}

func TestAuthService_Register_Validation(t *testing.T) {
	authSvc, _, billingServer := setupAuthService()
	defer billingServer.Close()

	ctx := context.Background()

	tests := []struct {
		name    string
		req     RegisterRequest
		wantErr string
	}{
		{
			name:    "empty org name",
			req:     RegisterRequest{Email: "a@b.com", Password: "password123", AcceptedTerms: true},
			wantErr: "organization name is required",
		},
		{
			name:    "invalid email",
			req:     RegisterRequest{OrganizationName: "Test", Email: "notemail", Password: "password123", AcceptedTerms: true},
			wantErr: "valid email is required",
		},
		{
			name:    "short password",
			req:     RegisterRequest{OrganizationName: "Test", Email: "a@b.com", Password: "short", AcceptedTerms: true},
			wantErr: "password must be at least 8 characters",
		},
		{
			name:    "terms not accepted",
			req:     RegisterRequest{OrganizationName: "Test", Email: "a@b.com", Password: "password123", AcceptedTerms: false},
			wantErr: "you must accept the terms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := authSvc.Register(ctx, &tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestAuthService_Register_DuplicateEmail(t *testing.T) {
	authSvc, repo, billingServer := setupAuthService()
	defer billingServer.Close()

	ctx := context.Background()
	repo.CreateOrganization(ctx, &models.Organization{
		Name:   "Existing",
		Email:  "existing@example.com",
		Status: "active",
	})

	_, err := authSvc.Register(ctx, &RegisterRequest{
		OrganizationName: "Test",
		Email:            "existing@example.com",
		Password:         "password123",
		AcceptedTerms:    true,
	})
	if err == nil || !strings.Contains(err.Error(), "email already registered") {
		t.Fatalf("expected duplicate email error, got %v", err)
	}
}

func TestAuthService_Register_Success(t *testing.T) {
	authSvc, repo, billingServer := setupAuthService()
	defer billingServer.Close()

	ctx := context.Background()
	resp, err := authSvc.Register(ctx, &RegisterRequest{
		OrganizationName: "New Org",
		Email:            "new@example.com",
		Password:         "password123",
		AcceptedTerms:    true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OrgID == 0 {
		t.Fatal("expected org_id")
	}
	if resp.UserID == 0 {
		t.Fatal("expected user_id")
	}

	// Verify org created
	org, _ := repo.GetOrganizationByID(ctx, resp.OrgID)
	if org == nil {
		t.Fatal("org not found")
	}
	if org.BillingAccountID == nil || *org.BillingAccountID != 999 {
		t.Fatalf("expected billing account id 999, got %v", org.BillingAccountID)
	}
}

func TestAuthService_Register_BillingAccountFailure(t *testing.T) {
	repo := NewMockRepository()
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer billingServer.Close()

	authSvc := NewAuthService(repo, billingServer.URL, "test-token")
	ctx := context.Background()

	resp, err := authSvc.Register(ctx, &RegisterRequest{
		OrganizationName: "New Org",
		Email:            "new2@example.com",
		Password:         "password123",
		AcceptedTerms:    true,
	})
	if err != nil {
		t.Fatalf("register should not fail when billing account creation fails: %v", err)
	}
	if resp.OrgID == 0 {
		t.Fatal("expected org_id")
	}
}

func TestAuthService_Login(t *testing.T) {
	authSvc, repo, billingServer := setupAuthService()
	defer billingServer.Close()

	ctx := context.Background()
	orgID, userID, _ := repo.SeedTestData()

	tests := []struct {
		name    string
		req     LoginRequest
		wantErr string
	}{
		{
			name:    "wrong email",
			req:     LoginRequest{Email: "wrong@example.com", Password: "password"},
			wantErr: "invalid credentials",
		},
		{
			name:    "wrong password",
			req:     LoginRequest{Email: "test@example.com", Password: "wrongpassword"},
			wantErr: "invalid credentials",
		},
		{
			name:    "inactive org",
			req:     LoginRequest{Email: "inactive@example.com", Password: "password"},
			wantErr: "account not active",
		},
		{
			name:    "unverified email",
			req:     LoginRequest{Email: "unverified@example.com", Password: "password"},
			wantErr: "email not verified",
		},
		{
			name:    "success",
			req:     LoginRequest{Email: "test@example.com", Password: "password"},
			wantErr: "",
		},
	}

	// Create inactive org
	repo.CreateOrganization(ctx, &models.Organization{
		Name:          "Inactive",
		Email:         "inactive@example.com",
		PasswordHash:  "$2a$10$kIxY6tX2MRiV4tROQZHKOenezw37Hdc1s14qDCSy9jsqBYFDP2Xde",
		Status:        "inactive",
		EmailVerified: true,
	})
	repo.CreateUser(ctx, &models.User{OrgID: orgID + 1, Email: "inactive@example.com", PasswordHash: "$2a$10$kIxY6tX2MRiV4tROQZHKOenezw37Hdc1s14qDCSy9jsqBYFDP2Xde", Role: "admin"})

	// Create unverified org
	repo.CreateOrganization(ctx, &models.Organization{
		Name:          "Unverified",
		Email:         "unverified@example.com",
		PasswordHash:  "$2a$10$kIxY6tX2MRiV4tROQZHKOenezw37Hdc1s14qDCSy9jsqBYFDP2Xde",
		Status:        "active",
		EmailVerified: false,
	})
	repo.CreateUser(ctx, &models.User{OrgID: orgID + 2, Email: "unverified@example.com", PasswordHash: "$2a$10$kIxY6tX2MRiV4tROQZHKOenezw37Hdc1s14qDCSy9jsqBYFDP2Xde", Role: "admin"})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := authSvc.Login(ctx, &tt.req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.SessionToken == "" {
				t.Fatal("expected session token")
			}
			if resp.User == nil || resp.User.ID != userID {
				t.Fatalf("expected user id %d, got %v", userID, resp.User)
			}
			if resp.ExpiresAt == "" {
				t.Fatal("expected expires_at")
			}
		})
	}
}

func TestAuthService_Logout(t *testing.T) {
	authSvc, repo, billingServer := setupAuthService()
	defer billingServer.Close()

	ctx := context.Background()
	_, _, token := repo.SeedTestData()

	err := authSvc.Logout(ctx, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = repo.GetSessionByToken(ctx, token)
	if err == nil {
		t.Fatal("expected session to be deleted")
	}
}

func TestAuthService_GetSession(t *testing.T) {
	authSvc, repo, billingServer := setupAuthService()
	defer billingServer.Close()

	ctx := context.Background()
	_, _, token := repo.SeedTestData()

	sess, err := authSvc.GetSession(ctx, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.Token != token {
		t.Fatalf("expected token %s, got %s", token, sess.Token)
	}
}

func TestAuthService_GetOrgByID(t *testing.T) {
	authSvc, repo, billingServer := setupAuthService()
	defer billingServer.Close()

	ctx := context.Background()
	orgID, _, _ := repo.SeedTestData()

	org, err := authSvc.GetOrgByID(ctx, orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if org.ID != orgID {
		t.Fatalf("expected org id %d, got %d", orgID, org.ID)
	}

	_, err = authSvc.GetOrgByID(ctx, 99999)
	if err == nil {
		t.Fatal("expected error for non-existent org")
	}
}

func TestAuthService_CreateBillingAccount_Error(t *testing.T) {
	repo := NewMockRepository()
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer billingServer.Close()

	authSvc := NewAuthService(repo, billingServer.URL, "test-token")
	ctx := context.Background()

	repo.SeedTestData()

	_, err := authSvc.CreateBillingAccount(ctx, 1)
	if err == nil {
		t.Fatal("expected error for billing failure")
	}
}

func TestAuthService_CreateBillingAccount_SetBillingError(t *testing.T) {
	repo := NewMockRepository()
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]int64{"id": 123})
	}))
	defer billingServer.Close()

	authSvc := NewAuthService(repo, billingServer.URL, "test-token")
	ctx := context.Background()

	// No org seeded, so SetBillingAccountID will silently fail (mock doesn't error)
	id, err := authSvc.CreateBillingAccount(ctx, 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 123 {
		t.Fatalf("expected billing account id 123, got %d", id)
	}
}

func TestAuthService_VerifyEmail(t *testing.T) {
	authSvc, _, billingServer := setupAuthService()
	defer billingServer.Close()

	ctx := context.Background()
	err := authSvc.VerifyEmail(ctx, "some-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuthService_Login_LastLoginUpdate(t *testing.T) {
	authSvc, repo, billingServer := setupAuthService()
	defer billingServer.Close()

	ctx := context.Background()
	_, userID, _ := repo.SeedTestData()

	_, err := authSvc.Login(ctx, &LoginRequest{Email: "test@example.com", Password: "password"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	user, _ := repo.GetUserByID(ctx, userID)
	if user.LastLoginAt == nil {
		t.Fatal("expected last_login_at to be updated")
	}
	if time.Since(*user.LastLoginAt) > time.Minute {
		t.Fatal("expected last_login_at to be recent")
	}
}
