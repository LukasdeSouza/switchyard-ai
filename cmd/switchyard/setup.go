package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/chzyer/readline"
)

// providerInfo holds metadata for each supported LLM provider.
type providerInfo struct {
	Name        string
	Key         string // internal identifier used in credentials
	KeyPrefix   string // expected key prefix for validation hint
	KeyGenURL   string // direct link to that provider's API key page
	Description string
}

var providers = []providerInfo{
	{
		Name:        "Gemini (Google)",
		Key:         "gemini",
		KeyPrefix:   "AIzaSy",
		KeyGenURL:   "https://aistudio.google.com/apikey",
		Description: "FREE tier available · Best for exploration",
	},
	{
		Name:        "OpenAI (GPT-4o)",
		Key:         "openai",
		KeyPrefix:   "sk-",
		KeyGenURL:   "https://platform.openai.com/api-keys",
		Description: "GPT-4o & mini · Great balance",
	},
	{
		Name:        "Anthropic (Claude)",
		Key:         "anthropic",
		KeyPrefix:   "sk-ant-",
		KeyGenURL:   "https://console.anthropic.com/settings/keys",
		Description: "Claude Sonnet · Best for coding tasks",
	},
	{
		Name:        "DeepSeek",
		Key:         "deepseek",
		KeyPrefix:   "sk-",
		KeyGenURL:   "https://platform.deepseek.com/api_keys",
		Description: "Budget-friendly · Strong reasoning",
	},
}

// RunSetupWizard is the interactive first-run setup wizard.
// It guides the user through authentication or BYOK key configuration.
func RunSetupWizard(baseURL string) {
	clearScreen()
	printSetupHeader()

	fmt.Println(bold("  How would you like to get started?"))
	fmt.Println()
	fmt.Printf("  %s  %s\n", cyan("[1]"), bold("Login via Switchyard (Recommended)"))
	fmt.Printf("       %s\n", dim("GitHub OAuth → smart routing, cost tracking, web dashboard"))
	fmt.Println()
	fmt.Printf("  %s  %s\n", cyan("[2]"), bold("Bring Your Own Key (BYOK)"))
	fmt.Printf("       %s\n", dim("Use your own provider API keys — no account needed"))
	fmt.Println()
	fmt.Printf("  %s  %s\n", dim("[3]"), dim("Skip setup — I'll configure manually later"))
	fmt.Println()
	fmt.Println(dim("  ────────────────────────────────────────────────────────"))
	fmt.Println()

	choice := promptChoice("  Enter choice [1-3]: ", []string{"1", "2", "3"})

	switch choice {
	case "1":
		runLoginFlow(baseURL)
	case "2":
		runBYOKFlow(baseURL)
	case "3":
		printSkipInstructions()
	}
}

// ─── Option 1: Login via Switchyard OAuth ────────────────────────────────────

func runLoginFlow(baseURL string) {
	fmt.Println()
	fmt.Println(bold(cyan("  Opening browser for GitHub OAuth authentication...")))
	fmt.Println()

	creds, err := StartWebAuthFlow(DefaultFrontendURL, baseURL)
	if err != nil {
		fmt.Println()
		fmt.Printf("  %s %s\n", red("Authentication failed:"), err.Error())
		fmt.Println()
		fmt.Printf("  You can try again later with: %s\n", yellow("switchyard login"))
		fmt.Println()
		return
	}

	// After login, offer to configure BYOK keys for cost savings
	fmt.Println()
	fmt.Println(bold(green("  ✓ Logged in successfully!")))
	if creds != nil && creds.ProjectID != "" {
		fmt.Printf("  Project: %s\n", cyan(creds.ProjectID))
	}
	fmt.Println()
	fmt.Println(dim("  ────────────────────────────────────────────────────────"))
	fmt.Println()
	fmt.Println(bold("  Optional: Add provider API keys for zero server-side costs"))
	fmt.Printf("  %s\n", dim("When BYOK keys are configured, requests go directly through"))
	fmt.Printf("  %s\n", dim("your own provider quotas — no Switchyard credits consumed."))
	fmt.Println()
	fmt.Printf("  Would you like to configure provider keys now? %s ", dim("[y/N]"))

	answer := readLine()
	if strings.ToLower(strings.TrimSpace(answer)) == "y" {
		runBYOKFlow(baseURL)
	} else {
		fmt.Println()
		fmt.Println(bold(green("  All set! Starting Switchyard...")))
		fmt.Println()
	}
}

// ─── Option 2: BYOK Wizard ───────────────────────────────────────────────────

func runBYOKFlow(_ string) {
	creds, _ := loadCredentials()
	if creds == nil {
		creds = &Credentials{}
	}
	if creds.ProviderKeys == nil {
		creds.ProviderKeys = make(map[string]string)
	}

	configured := 0

	for {
		clearScreen()
		printSetupHeader()

		fmt.Println(bold("  Configure provider API keys (BYOK)"))
		fmt.Println(dim("  Keys are stored locally in ~/.switchyard/credentials.json"))
		fmt.Println()

		// List providers with current status
		for i, p := range providers {
			status := dim("[NOT SET]")
			if v := creds.ProviderKeys[p.Key]; v != "" {
				masked := maskKey(v)
				status = green("[CONFIGURED]") + dim(" ("+masked+")")
			} else if envVal := os.Getenv(strings.ToUpper(p.Key) + "_API_KEY"); envVal != "" {
				status = cyan("[ENV SET]")
			}
			fmt.Printf("  %s  %-24s %s\n",
				cyan(fmt.Sprintf("[%d]", i+1)),
				bold(p.Name),
				status,
			)
			fmt.Printf("       %s\n", dim(p.Description))
			fmt.Println()
		}

		fmt.Printf("  %s  %s\n", dim("[5]"), dim("Done — start using Switchyard"))
		fmt.Println()
		fmt.Println(dim("  ────────────────────────────────────────────────────────"))
		fmt.Println()

		choice := promptChoice("  Choose a provider to configure [1-5]: ", []string{"1", "2", "3", "4", "5"})
		idx := int(choice[0]-'0') - 1

		if choice == "5" {
			break
		}

		if idx < 0 || idx >= len(providers) {
			continue
		}

		if configureProviderKey(creds, providers[idx]) {
			if err := saveCredentials(creds); err != nil {
				fmt.Printf("\n  %s Could not save credentials: %v\n", red("Error:"), err)
			} else {
				configured++
			}
		}
	}

	fmt.Println()
	if configured > 0 {
		fmt.Printf("  %s %d provider key(s) configured.\n", bold(green("✓")), configured)
	}

	if creds.APIKey == "" {
		fmt.Println()
		fmt.Println(dim("  Tip: Run 'switchyard login' to link a project for full routing,"))
		fmt.Println(dim("  cost tracking, and web dashboard access."))
	}

	fmt.Println()
	fmt.Println(bold(green("  All set! Starting Switchyard...")))
	fmt.Println()
}

// configureProviderKey walks the user through configuring a single provider API key.
// Returns true if a key was successfully saved.
func configureProviderKey(creds *Credentials, p providerInfo) bool {
	fmt.Println()
	fmt.Printf("  %s\n", bold(cyan("Configuring: "+p.Name)))
	fmt.Println()
	fmt.Printf("  1. We'll open your browser to the %s API key page.\n", p.Name)
	fmt.Printf("  2. Create or copy your API key there.\n")
	fmt.Printf("  3. Paste it here when ready.\n")
	fmt.Println()
	fmt.Printf("  URL: %s\n", yellow(p.KeyGenURL))
	fmt.Println()
	fmt.Printf("  Press %s to open browser, or %s to skip: ", bold("[Enter]"), dim("[s]"))

	input := readLine()
	if strings.ToLower(strings.TrimSpace(input)) == "s" {
		return false
	}

	// Open browser to provider's API key page
	if err := openBrowser(p.KeyGenURL); err != nil {
		fmt.Printf("  %s Could not open browser automatically.\n", dim("(note)"))
		fmt.Printf("  Please visit: %s\n", yellow(p.KeyGenURL))
	} else {
		fmt.Printf("  %s Browser opened. Create your key and come back here.\n", green("→"))
	}

	fmt.Println()

	// Masked input for the API key
	apiKey, err := readMaskedInput(fmt.Sprintf("  Paste your %s API key: ", p.Name))
	if err != nil || strings.TrimSpace(apiKey) == "" {
		fmt.Printf("  %s No key entered. Skipping.\n", yellow("→"))
		return false
	}
	apiKey = strings.TrimSpace(apiKey)

	// Basic prefix validation (hint only, not hard block)
	if p.KeyPrefix != "" && !strings.HasPrefix(apiKey, p.KeyPrefix) {
		fmt.Printf("\n  %s Key doesn't start with '%s'. This may be incorrect.\n",
			yellow("⚠"), p.KeyPrefix)
		fmt.Printf("  Continue anyway? %s ", dim("[y/N]"))
		confirm := readLine()
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			return false
		}
	}

	// Validate the key against the provider API (best-effort, non-blocking)
	fmt.Printf("\n  %s Validating key...", dim("→"))
	valid, validationMsg := validateProviderKey(p.Key, apiKey)
	if valid {
		fmt.Printf(" %s\n", green("✓ Key is valid!"))
	} else {
		fmt.Printf(" %s\n", yellow("(could not validate — will save anyway)"))
		if validationMsg != "" {
			fmt.Printf("  %s %s\n", dim("Hint:"), dim(validationMsg))
		}
	}

	creds.ProviderKeys[p.Key] = apiKey

	fmt.Printf("\n  %s Key for %s saved.\n", green("✓"), bold(p.Name))
	fmt.Println()
	fmt.Printf("  Press %s to continue...", dim("[Enter]"))
	readLine()

	return true
}

// validateProviderKey makes a lightweight test call to check if the key is accepted.
// Returns (true, "") on success, or (false, hint) on failure.
func validateProviderKey(provider, key string) (bool, string) {
	client := &http.Client{Timeout: 6 * time.Second}

	switch provider {
	case "gemini":
		url := "https://generativelanguage.googleapis.com/v1beta/models?key=" + key
		resp, err := client.Get(url)
		if err != nil {
			return false, "network error"
		}
		defer resp.Body.Close()
		return resp.StatusCode == 200, fmt.Sprintf("HTTP %d", resp.StatusCode)

	case "openai":
		req, _ := http.NewRequest("GET", "https://api.openai.com/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := client.Do(req)
		if err != nil {
			return false, "network error"
		}
		defer resp.Body.Close()
		return resp.StatusCode == 200, fmt.Sprintf("HTTP %d", resp.StatusCode)

	case "anthropic":
		req, _ := http.NewRequest("GET", "https://api.anthropic.com/v1/models", nil)
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
		resp, err := client.Do(req)
		if err != nil {
			return false, "network error"
		}
		defer resp.Body.Close()
		return resp.StatusCode == 200, fmt.Sprintf("HTTP %d", resp.StatusCode)

	default:
		// No validation available for this provider — skip
		return false, ""
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func printSetupHeader() {
	fmt.Println()
	fmt.Println(bold(cyan("  ╔══════════════════════════════════════════════════════╗")))
	fmt.Println(bold(cyan("  ║      SWITCHYARD  ·  Welcome! First-Run Setup         ║")))
	fmt.Println(bold(cyan("  ╚══════════════════════════════════════════════════════╝")))
	fmt.Println()
}

func printSkipInstructions() {
	fmt.Println()
	fmt.Println(dim("  No problem. You can configure Switchyard anytime:"))
	fmt.Println()
	fmt.Printf("  %s  %s\n", yellow("switchyard login"), dim("  → GitHub OAuth (full platform access)"))
	fmt.Printf("  %s  %s\n", yellow("switchyard setup"), dim("  → Re-run this wizard"))
	fmt.Printf("  %s  %s\n", yellow("switchyard keys set <provider> <key>"), dim("→ Set a specific key"))
	fmt.Println()
	fmt.Println(dim("  Documentation: https://switchyard-docs--lukasdesouza.replit.app/"))
	fmt.Println()
}

// promptChoice reads a single-character selection from stdin, validates it against allowed.
func promptChoice(promptText string, allowed []string) string {
	allowedSet := make(map[string]bool)
	for _, a := range allowed {
		allowedSet[a] = true
	}

	rl, err := readline.New(promptText)
	if err != nil {
		// Fallback to bufio if readline fails
		fmt.Print(promptText)
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			v := strings.TrimSpace(scanner.Text())
			if allowedSet[v] {
				return v
			}
		}
		return allowed[0]
	}
	defer rl.Close()

	for {
		line, err := rl.Readline()
		if err != nil {
			return allowed[len(allowed)-1]
		}
		v := strings.TrimSpace(line)
		if allowedSet[v] {
			return v
		}
		fmt.Printf("  %s Please enter one of: %s\n", red("→"), strings.Join(allowed, ", "))
	}
}

// readLine reads a plain line of input from stdin.
func readLine() string {
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return scanner.Text()
	}
	return ""
}

// readMaskedInput uses readline to read a password-style masked input.
func readMaskedInput(prompt string) (string, error) {
	rl, err := readline.NewEx(&readline.Config{
		Prompt:                 prompt,
		EnableMask:             true,
		MaskRune:               '*',
		DisableAutoSaveHistory: true,
	})
	if err != nil {
		// Fallback: unmasked input
		fmt.Print(prompt)
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			return scanner.Text(), nil
		}
		return "", fmt.Errorf("read failed")
	}
	defer rl.Close()

	line, err := rl.Readline()
	return line, err
}

// maskKey returns a partially masked version of an API key for display.
func maskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:6] + "..." + key[len(key)-4:]
}

// clearScreen clears the terminal (best-effort).
func clearScreen() {
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("cmd", "/c", "cls")
		cmd.Stdout = os.Stdout
		_ = cmd.Run()
	default:
		fmt.Print("\033[H\033[2J")
	}
}
