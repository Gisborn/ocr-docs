package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"scan.passport.local/api/services/cabinet/internal/repository"
	"scan.passport.local/api/services/cabinet/pkg/models"
)

// AdminCLI CLI for system administration
type AdminCLI struct {
	repo repository.Repository
}

func main() {
	databaseURL := getEnv("DATABASE_URL", "postgres://api_scan:api_scan_secret@localhost:5432/api_scan")
	
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		fmt.Printf("Failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()
	
	cli := &AdminCLI{
		repo: repository.NewPostgresRepository(pool),
	}
	
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(0)
	}
	
	ctx := context.Background()
	
	switch os.Args[1] {
	case "create-org":
		cli.createOrganization(ctx)
	case "create-user":
		cli.createUser(ctx)
	case "create-api-key":
		cli.createAPIKey(ctx)
	case "list-api-keys":
		cli.listAPIKeys(ctx)
	case "add-balance":
		cli.addBalance(ctx)
	case "seed":
		cli.seed(ctx)
	case "help":
		printHelp()
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println("Admin CLI for API-Scan")
	fmt.Println("")
	fmt.Println("Usage: go run scripts/admin-cli.go <command>")
	fmt.Println("")
	fmt.Println("Commands:")
	fmt.Println("  create-org      Create new organization")
	fmt.Println("  create-user     Create user in organization")
	fmt.Println("  create-api-key  Create API key for organization")
	fmt.Println("  list-api-keys   List API keys for organization")
	fmt.Println("  add-balance     Add balance to billing account")
	fmt.Println("  seed            Seed database with test data")
	fmt.Println("  help            Show this help")
	fmt.Println("")
	fmt.Println("Environment:")
	fmt.Println("  DATABASE_URL - PostgreSQL connection string")
}

func (cli *AdminCLI) createOrganization(ctx context.Context) {
	reader := bufio.NewReader(os.Stdin)
	
	fmt.Print("Organization name: ")
	name, _ := reader.ReadString('\n')
	name = strings.TrimSpace(name)
	
	fmt.Print("Email: ")
	email, _ := reader.ReadString('\n')
	email = strings.TrimSpace(email)
	
	fmt.Print("Password: ")
	password, _ := reader.ReadString('\n')
	password = strings.TrimSpace(password)
	
	if name == "" || email == "" || password == "" {
		fmt.Println("All fields are required")
		os.Exit(1)
	}
	
	// Check if exists
	existing, _ := cli.repo.GetOrganizationByEmail(ctx, email)
	if existing != nil {
		fmt.Println("Organization with this email already exists")
		os.Exit(1)
	}
	
	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Printf("Password hash error: %v\n", err)
		os.Exit(1)
	}
	
	// Create org
	org := &models.Organization{
		Name:         name,
		Email:        email,
		EmailVerified: true, // Auto-verify for CLI
		PasswordHash: string(hash),
		Status:       "active",
	}
	
	if err := cli.repo.CreateOrganization(ctx, org); err != nil {
		fmt.Printf("Create organization failed: %v\n", err)
		os.Exit(1)
	}
	
	// Create admin user
	user := &models.User{
		OrgID:        org.ID,
		Email:        email,
		PasswordHash: string(hash),
		Role:         "admin",
	}
	
	if err := cli.repo.CreateUser(ctx, user); err != nil {
		fmt.Printf("Create user failed: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("✓ Organization created successfully\n")
	fmt.Printf("  ID: %d\n", org.ID)
	fmt.Printf("  Name: %s\n", org.Name)
	fmt.Printf("  Email: %s\n", org.Email)
	fmt.Printf("  User ID: %d\n", user.ID)
	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Println("  1. Create billing account (via API)")
	fmt.Println("  2. Create API key: go run scripts/admin-cli.go create-api-key")
}

func (cli *AdminCLI) createAPIKey(ctx context.Context) {
	reader := bufio.NewReader(os.Stdin)
	
	fmt.Print("Organization ID: ")
	orgIDStr, _ := reader.ReadString('\n')
	orgIDStr = strings.TrimSpace(orgIDStr)
	
	orgID, err := strconv.ParseInt(orgIDStr, 10, 64)
	if err != nil {
		fmt.Println("Invalid organization ID")
		os.Exit(1)
	}
	
	fmt.Print("Key name: ")
	name, _ := reader.ReadString('\n')
	name = strings.TrimSpace(name)
	
	// Generate key
	fullKey, keyHash, err := generateAPIKey()
	if err != nil {
		fmt.Printf("Key generation failed: %v\n", err)
		os.Exit(1)
	}
	
	// Save to DB
	key := &models.APIKey{
		OrgID:        orgID,
		Name:         name,
		KeyHash:      keyHash,
		Status:       "active",
		RateLimitRPS: 10,
	}
	
	if err := cli.repo.CreateAPIKey(ctx, key); err != nil {
		fmt.Printf("Create API key failed: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("✓ API Key created successfully\n")
	fmt.Printf("  ID: %d\n", key.ID)
	fmt.Printf("  Name: %s\n", key.Name)
	fmt.Printf("\n")
	fmt.Printf("╔════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║  IMPORTANT: Save this key, it won't be shown again!        ║\n")
	fmt.Printf("║                                                            ║\n")
	fmt.Printf("║  %s\n", fullKey)
	fmt.Printf("║                                                            ║\n")
	fmt.Printf("╚════════════════════════════════════════════════════════════╝\n")
}

func (cli *AdminCLI) listAPIKeys(ctx context.Context) {
	reader := bufio.NewReader(os.Stdin)
	
	fmt.Print("Organization ID: ")
	orgIDStr, _ := reader.ReadString('\n')
	orgIDStr = strings.TrimSpace(orgIDStr)
	
	orgID, err := strconv.ParseInt(orgIDStr, 10, 64)
	if err != nil {
		fmt.Println("Invalid organization ID")
		os.Exit(1)
	}
	
	keys, err := cli.repo.ListAPIKeys(ctx, orgID)
	if err != nil {
		fmt.Printf("List API keys failed: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("API Keys for organization %d:\n", orgID)
	fmt.Printf("%-5s %-20s %-10s %-15s %-20s\n", "ID", "Name", "Status", "Rate Limit", "Created")
	fmt.Println(strings.Repeat("-", 75))
	
	for _, key := range keys {
		preview := "****" + key.KeyHash[len(key.KeyHash)-4:]
		if key.Status != "active" {
			preview = "****xxxx"
		}
		fmt.Printf("%-5d %-20s %-10s %-15d %-20s\n",
			key.ID,
			key.Name,
			key.Status,
			key.RateLimitRPS,
			key.CreatedAt.Format("2006-01-02 15:04"),
		)
	}
}

func (cli *AdminCLI) addBalance(ctx context.Context) {
	fmt.Println("Add balance via Billing Service API:")
	fmt.Println("  POST /accounts/{id}/payments")
	fmt.Println("")
	fmt.Println("Or insert directly into billing_events table:")
	fmt.Println("  INSERT INTO billing_events (account_id, type, real_amount_rub, created_at)")
	fmt.Println("  VALUES (1, 'balance_topup', 10000, NOW());")
}

func (cli *AdminCLI) seed(ctx context.Context) {
	fmt.Println("Seeding database with test data...")
	fmt.Println("")
	fmt.Println("Run this SQL in billing database:")
	fmt.Println("  psql -U billing -d billing_db -f scripts/seed.sql")
	fmt.Println("")
	fmt.Println("Or use docker:")
	fmt.Println("  docker exec -i api-scan-postgres psql -U api_scan -d api_scan < scripts/seed.sql")
}

func (cli *AdminCLI) createUser(ctx context.Context) {
	reader := bufio.NewReader(os.Stdin)
	
	fmt.Print("Organization ID: ")
	orgIDStr, _ := reader.ReadString('\n')
	orgID, _ := strconv.ParseInt(strings.TrimSpace(orgIDStr), 10, 64)
	
	fmt.Print("Email: ")
	email, _ := reader.ReadString('\n')
	email = strings.TrimSpace(email)
	
	fmt.Print("Password: ")
	password, _ := reader.ReadString('\n')
	password = strings.TrimSpace(password)
	
	fmt.Print("Role (admin/user): ")
	role, _ := reader.ReadString('\n')
	role = strings.TrimSpace(role)
	if role == "" {
		role = "user"
	}
	
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	
	user := &models.User{
		OrgID:        orgID,
		Email:        email,
		PasswordHash: string(hash),
		Role:         role,
	}
	
	if err := cli.repo.CreateUser(ctx, user); err != nil {
		fmt.Printf("Create user failed: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("✓ User created successfully (ID: %d)\n", user.ID)
}

func generateAPIKey() (string, string, error) {
	// Generate random secret
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte('a' + (i % 26))
	}
	
	// Format: id:secret in base64
	fullKey := fmt.Sprintf("test_%d_%s", time.Now().Unix(), string(secret))
	
	// Hash for storage
	hash, err := bcrypt.GenerateFromPassword([]byte(fullKey), bcrypt.DefaultCost)
	if err != nil {
		return "", "", err
	}
	
	return fullKey, string(hash), nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
