// main.go — Sajon Cloud Compiler entry point  ·  v1.1.0
//
// Usage:
//
//	sajon up [file.saj]      compile + provision (default file: app.saj)
//	sajon plan [file.saj]    dry-run: preview what 'up' will do
//	sajon down               destroy all active cloud resources
//	sajon ci github          generate GitHub Actions deploy workflow
//	sajon help               show usage
//	sajon version            show compiler version
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"sajon/pkg/emitter"
	"sajon/pkg/lexer"
	"sajon/pkg/parser"
)

// ── ANSI colour helpers ───────────────────────────────────────────────────────
const (
	ansiReset      = "\033[0m"
	ansiBold       = "\033[1m"
	ansiRed        = "\033[31m"
	ansiGreen      = "\033[32m"
	ansiYellow     = "\033[33m"
	ansiCyan       = "\033[36m"
	ansiMagenta    = "\033[35m"
	ansiWhite      = "\033[97m"
	ansiBoldRed    = "\033[1;31m"
	ansiBoldGreen  = "\033[1;32m"
	ansiBoldCyan   = "\033[1;36m"
	ansiBoldYellow   = "\033[1;33m"
	ansiBoldMagenta  = "\033[1;35m"
	ansiDim          = "\033[2m"
)

func colorize(color, text string) string { return color + text + ansiReset }

// ── Constants ─────────────────────────────────────────────────────────────────

const (
	version       = "1.1.0"
	defaultSource = "app.saj"
	outputFile    = "docker-compose.yml"
)

// ── Entry point ───────────────────────────────────────────────────────────────

func main() {
	args := os.Args[1:] // strip the binary name

	if len(args) == 0 {
		printUsage()
		os.Exit(0)
	}

	// ── Init remote state FIRST — before any lock file access ─────────────────
	initRemoteState()

	// ── Git Ignore Guard — runs on every substantive command at boot ──────────
	if args[0] != "help" && args[0] != "--help" && args[0] != "-h" &&
		args[0] != "version" && args[0] != "--version" && args[0] != "-v" {
		runGitIgnoreGuard()
	}

	// ── Windows Shell Sync — silent .saj file association at boot ─────────────
	// Only fires for substantive commands. On non-Windows this is a no-op.
	// runWindowsShellSync() returns true ONLY on the very first successful
	// registration, so the "Environment Synced" message appears just once.
	if args[0] != "help" && args[0] != "--help" && args[0] != "-h" &&
		args[0] != "version" && args[0] != "--version" && args[0] != "-v" {
		if runWindowsShellSync() {
			fmt.Printf("  %s  %s\n\n",
				colorize(ansiBoldMagenta, "[🎨]"),
				colorize(ansiBoldMagenta, "Sajon Environment Synced: Custom .saj file icons are now active on your OS!"))
		}
	}

	switch args[0] {
	case "help", "--help", "-h":
		printUsage()
		os.Exit(0)
	case "version", "--version", "-v":
		fmt.Printf("sajon version %s\n", version)
		os.Exit(0)
	case "up":
		sourceFile := defaultSource
		force := false
		for _, a := range args[1:] {
			if a == "--force" {
				force = true
			} else if !strings.HasPrefix(a, "-") {
				sourceFile = a
			}
		}
		runPipeline(sourceFile, force)
	case "plan":
		sourceFile := defaultSource
		force := false
		for _, a := range args[1:] {
			if a == "--force" {
				force = true
			} else if !strings.HasPrefix(a, "-") {
				sourceFile = a
			}
		}
		runPlan(sourceFile, force)
	case "down":
		force := false
		for _, a := range args[1:] {
			if a == "--force" {
				force = true
			}
		}
		runDown(force)
	case "ci":
		// sajon ci github [file.saj]
		if len(args) < 2 {
			fmt.Printf("\n  %s  Usage: sajon ci <provider> [file.saj]\n",
				colorize(ansiBoldRed, "✖"))
			fmt.Printf("  %s  Supported providers: %s\n\n",
				colorize(ansiCyan, "→"), colorize(ansiBold, "github"))
			os.Exit(1)
		}
		provider := args[1]
		sajFile := defaultSource
		if len(args) >= 3 && !strings.HasPrefix(args[2], "-") {
			sajFile = args[2]
		}
		runCIInit(provider, sajFile)
	default:
		fmt.Printf("\n  %s  Unknown command '%s'\n\n",
			colorize(ansiBoldRed, "x"), args[0])
		printUsage()
		os.Exit(1)
	}
}

// initRemoteState reads SAJON_REMOTE_BUCKET + AWS credentials and enables
// S3-backed state storage when all three are present.
func initRemoteState() {
	bucket    := os.Getenv("SAJON_REMOTE_BUCKET")
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	region    := os.Getenv("AWS_DEFAULT_REGION")

	if bucket != "" && accessKey != "" && secretKey != "" {
		emitter.ConfigureRemoteState(bucket, region, accessKey, secretKey)
	}
}

// ── runGitIgnoreGuard ─────────────────────────────────────────────────────────
//
// runGitIgnoreGuard is the zero-configuration security layer that fires at
// boot (Phase 1) for every substantive Sajon command.
//
// It performs three steps:
//  1. Auto-detect / create .gitignore in the current working directory.
//  2. Ensure the three mandatory secret-exclusion entries are present;
//     append any that are missing (append-only, never overwrites existing).
//  3. Warn when live cloud credentials are loaded into shell memory while
//     sajon.env exists on disk — reminding the user that the file is
//     already git-protected.
//
// Required .gitignore entries:
//   - sajon.keys
//   - sajon.env
//   - .env
func runGitIgnoreGuard() {
	const giPath = ".gitignore"

	// ── Required entries ──────────────────────────────────────────────────
	required := []string{"sajon.keys", "sajon.env", ".env"}

	// ── Read existing .gitignore (or treat as empty) ──────────────────────
	var existing string
	raw, readErr := os.ReadFile(giPath)
	if readErr == nil {
		existing = string(raw)
	}

	// ── Check which entries are missing ───────────────────────────────────
	var missing []string
	for _, entry := range required {
		// Match the entry as a standalone line (exact word boundary check)
		// to avoid false positives like "my-sajon.env".
		found := false
		for _, line := range strings.Split(existing, "\n") {
			if strings.TrimSpace(line) == entry {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, entry)
		}
	}

	// ── If file doesn't exist, create it from scratch ─────────────────────
	if readErr != nil && len(missing) > 0 {
		header := "# ── Sajon Auto-Generated .gitignore ──────────────────────────────────\n"
		header += "# Generated automatically by Sajon Git-Ignore Guard.\n"
		header += "# These entries protect your cloud credentials from being committed.\n\n"
		content := header
		for _, e := range missing {
			content += e + "\n"
		}
		if writeErr := os.WriteFile(giPath, []byte(content), 0644); writeErr != nil {
			fmt.Printf("  %s  Git-Ignore Guard: could not create .gitignore: %s\n",
				colorize(ansiBoldYellow, "[⚠️]"), writeErr.Error())
			return
		}
		for _, e := range missing {
			fmt.Printf("  %s  Git-Ignore Guard: .gitignore created — added entry '%s'\n",
				colorize(ansiBoldGreen, "[🛡️]"), e)
		}
	} else if len(missing) > 0 {
		// ── Append missing entries to existing file ───────────────────────
		f, openErr := os.OpenFile(giPath, os.O_APPEND|os.O_WRONLY, 0644)
		if openErr != nil {
			fmt.Printf("  %s  Git-Ignore Guard: could not update .gitignore: %s\n",
				colorize(ansiBoldYellow, "[⚠️]"), openErr.Error())
			return
		}
		defer f.Close()

		// Ensure we start on a fresh line
		if len(existing) > 0 && !strings.HasSuffix(existing, "\n") {
			f.WriteString("\n") //nolint:errcheck
		}
		f.WriteString("\n# ── Sajon Git-Ignore Guard (auto-appended) ─────────────────────────\n") //nolint:errcheck
		for _, e := range missing {
			f.WriteString(e + "\n") //nolint:errcheck
			fmt.Printf("  %s  Git-Ignore Guard: appended missing entry '%s' to .gitignore\n",
				colorize(ansiBoldGreen, "[🛡️]"), e)
		}
	}

	// ── Active Sync Warning ───────────────────────────────────────────────
	// If live cloud credentials are in shell memory AND sajon.env exists
	// on disk, remind the user that sajon.env is git-protected.
	supaToken := os.Getenv("SUPABASE_ACCESS_TOKEN")
	neonKey   := os.Getenv("NEON_API_KEY")
	awsKey    := os.Getenv("AWS_ACCESS_KEY_ID")

	_, envStat := os.Stat("sajon.env")
	envExists := envStat == nil

	if envExists && (supaToken != "" || neonKey != "" || awsKey != "") {
		var liveVars []string
		if supaToken != "" { liveVars = append(liveVars, "SUPABASE_ACCESS_TOKEN") }
		if neonKey != ""   { liveVars = append(liveVars, "NEON_API_KEY") }
		if awsKey != ""    { liveVars = append(liveVars, "AWS_ACCESS_KEY_ID") }
		fmt.Printf("  %s  %s %s %s\n",
			colorize(ansiBoldGreen, "[🛡️]"),
			colorize(ansiGreen, "Credentials in memory ("+strings.Join(liveVars, ", ")+") detected —"),
			colorize(ansiBoldGreen, "sajon.env"),
			colorize(ansiGreen, "is git-locked and protected from accidental commit."))
	}

	// ── Always print the guard-active banner ─────────────────────────────
	fmt.Printf("  %s  %s\n\n",
		colorize(ansiBoldGreen, "[🛡️]"),
		colorize(ansiBoldGreen, "Git-Ignore Guard Active: sajon.env, sajon.keys, and .env are protected."))
}

// printUsage prints the CLI help text.
func printUsage() {
	sep := strings.Repeat("-", 62)
	fmt.Printf("\n%s\n", colorize(ansiBoldCyan, sep))
	fmt.Printf("  %s  %s\n",
		colorize(ansiBoldCyan, "SAJON"),
		colorize(ansiWhite, "Cloud-native language compiler  v"+version))
	fmt.Printf("%s\n\n", colorize(ansiBoldCyan, sep))
	fmt.Printf("  %s\n\n", colorize(ansiBold, "USAGE"))
	fmt.Printf("    %s  %s\n",
		colorize(ansiGreen, "sajon up [file.saj] [--force]"),
		colorize(ansiDim, "compile + provision infrastructure (default: app.saj)"))
	fmt.Printf("    %s  %s\n",
		colorize(ansiGreen, "sajon plan [file.saj] [--force]"),
		colorize(ansiDim, "dry-run: preview what 'up' will do (no API calls)"))
	fmt.Printf("    %s  %s\n",
		colorize(ansiRed, "sajon down [--force]           "),
		colorize(ansiDim, "destroy all active cloud resources in sajon.lock"))
	fmt.Printf("    %s  %s\n",
		colorize(ansiBoldMagenta, "sajon ci github [file.saj]     "),
		colorize(ansiMagenta, "auto-generate GitHub Actions CI/CD deploy workflow"))
	fmt.Printf("    %s  %s\n",
		colorize(ansiGreen, "sajon help                     "),
		colorize(ansiDim, "show this help message"))
	fmt.Printf("    %s  %s\n",
		colorize(ansiGreen, "sajon version                  "),
		colorize(ansiDim, "print compiler version"))
	fmt.Printf("\n  %s\n\n", colorize(ansiBold, "FLAGS"))
	fmt.Printf("    %s  %s\n",
		colorize(ansiBoldYellow, "--force"),
		colorize(ansiDim, "bypass the rename/orphan safety guard (use with caution)"))
	fmt.Printf("\n  %s\n\n", colorize(ansiBold, "ENVIRONMENT"))
	fmt.Printf("    %s  %s\n",
		colorize(ansiYellow, "NEON_API_KEY          "),
		colorize(ansiDim, "Neon API key -- enables real Neon Postgres provisioning"))
	fmt.Printf("    %s  %s\n",
		colorize(ansiYellow, "NEON_ORG_ID           "),
		colorize(ansiDim, "Neon organisation ID (auto-detected when omitted)"))
	fmt.Printf("    %s  %s\n",
		colorize(ansiBoldYellow, "SAJON_REMOTE_BUCKET   "),
		colorize(ansiBoldGreen, "S3 bucket for remote sajon.lock (multi-user sync)"))
	fmt.Printf("    %s  %s\n",
		colorize(ansiBoldYellow, "AWS_ACCESS_KEY_ID     "),
		colorize(ansiDim, "AWS credential (required with SAJON_REMOTE_BUCKET)"))
	fmt.Printf("    %s  %s\n",
		colorize(ansiBoldYellow, "AWS_SECRET_ACCESS_KEY "),
		colorize(ansiDim, "AWS credential (required with SAJON_REMOTE_BUCKET)"))
	fmt.Printf("    %s  %s\n",
		colorize(ansiYellow, "AWS_DEFAULT_REGION    "),
		colorize(ansiDim, "AWS region for S3 state bucket (default: us-east-1)"))
	fmt.Printf("\n%s\n\n", colorize(ansiBoldCyan, sep))
}

// runPipeline is the core six-phase compiler pipeline, now driven by
// a sourceFile argument instead of a hardcoded constant.
func runPipeline(sourceFile string, force bool) {
	sep  := strings.Repeat("─", 62)
	sep2 := strings.Repeat("━", 62)
	t0   := time.Now()

	// ── Banner ────────────────────────────────────────────────────────────────
	fmt.Printf("\n%s\n", colorize(ansiBoldCyan, sep2))
	fmt.Printf("  %s\n", colorize(ansiBoldCyan, "SAJON Cloud Compiler  ·  v"+version))
	fmt.Printf("  %s\n", colorize(ansiWhite, "Cloud-native language engine — Full Pipeline"))
	fmt.Printf("%s\n\n", colorize(ansiBoldCyan, sep2))

	// ── S3 / Local state mode banner ─────────────────────────────────────
	// Show remote state mode banner
	if emitter.IsRemoteStateEnabled() {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiBoldGreen, "[S3 STATE]"),
			colorize(ansiBoldGreen, fmt.Sprintf("REMOTE STATE ENABLED -- s3://%s/sajon.lock (team sync active)", emitter.RemoteStateBucket())))
	} else {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiYellow, "[local]"),
			colorize(ansiDim, "State: local sajon.lock (set SAJON_REMOTE_BUCKET for multi-user sync)"))
	}

	_, _ = emitter.ReadLockFile()


	// ── NEON_API_KEY detection ─────────────────────────────────────────────────
	neonKey := os.Getenv("NEON_API_KEY")
	if neonKey == "" {
		printLocalModeWarning(sep)
	} else {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiGreen, "☁"),
			colorize(ansiBoldGreen, "NEON_API_KEY detected — cloud provisioning ENABLED"))
	}

	// ═══════════════════════════════════════════════════════════════════════
	// PHASE 1 — Read source file
	// ═══════════════════════════════════════════════════════════════════════
	absSource, _ := filepath.Abs(sourceFile)
	fmt.Printf("  %s  %s\n",
		colorize(ansiBoldYellow, "①"),
		colorize(ansiYellow, "Reading source file..."))
	fmt.Printf("     %s\n\n", colorize(ansiDim, absSource))

	src, err := os.ReadFile(sourceFile)
	if err != nil {
		fmt.Printf("  %s  Cannot open '%s': %s\n\n",
			colorize(ansiBoldRed, "✖  ERROR"),
			colorize(ansiWhite, sourceFile),
			colorize(ansiRed, err.Error()))
		fmt.Printf("  %s  Create an '%s' file in the project root and re-run.\n\n",
			colorize(ansiCyan, "→"), colorize(ansiWhite, sourceFile))
		os.Exit(1)
	}

	lines := strings.Count(string(src), "\n")
	fmt.Printf("  %s  Loaded %s  —  %s, %s\n\n",
		colorize(ansiGreen, "✔"),
		colorize(ansiBold, sourceFile),
		colorize(ansiCyan, fmt.Sprintf("%d bytes", len(src))),
		colorize(ansiCyan, fmt.Sprintf("%d lines", lines)))

	// ═══════════════════════════════════════════════════════════════════════
	// PHASE 2 — Lex
	// ═══════════════════════════════════════════════════════════════════════
	fmt.Printf("  %s  %s\n",
		colorize(ansiBoldYellow, "②"),
		colorize(ansiYellow, "Tokenising source..."))

	l := lexer.New(string(src))
	tokenCount := countTokens(string(src))
	fmt.Printf("  %s  %s\n\n",
		colorize(ansiGreen, "✔"),
		colorize(ansiCyan, fmt.Sprintf("%d tokens produced", tokenCount)))

	// ═══════════════════════════════════════════════════════════════════════
	// PHASE 3 — Parse
	// ═══════════════════════════════════════════════════════════════════════
	fmt.Printf("  %s  %s\n",
		colorize(ansiBoldYellow, "③"),
		colorize(ansiYellow, "Running recursive-descent parser..."))

	p := parser.New(l)
	program := p.ParseProgram()

	if errs := p.Errors(); len(errs) > 0 {
		fmt.Printf("\n  %s\n\n",
			colorize(ansiBoldRed, "✖  PARSE FAILED — syntax error(s) in "+sourceFile+":"))
		for i, e := range errs {
			fmt.Printf("  %s  %s\n",
				colorize(ansiRed, fmt.Sprintf("[%d]", i+1)),
				colorize(ansiBoldRed, e))
		}
		fmt.Println()
		os.Exit(1)
	}

	fmt.Printf("  %s  %s\n\n",
		colorize(ansiGreen, "✔"),
		colorize(ansiCyan, fmt.Sprintf("AST built — %d top-level statement(s)", len(program.Statements))))

	// ── AST Tree dump ─────────────────────────────────────────────────────
	fmt.Printf("%s\n", sep)
	fmt.Printf("  %s\n", colorize(ansiBold, "AST DUMP"))
	fmt.Printf("%s\n\n", sep)
	printAST(program)
	fmt.Println()

	// ═══════════════════════════════════════════════════════════════════════
	// PHASE 4 — Local Emit (docker-compose.yml)
	// ═══════════════════════════════════════════════════════════════════════
	fmt.Printf("%s\n", sep)
	fmt.Printf("  %s  %s\n\n",
		colorize(ansiBoldYellow, "④"),
		colorize(ansiYellow, "Generating docker-compose.yml..."))

	em := emitter.New(program)
	yamlOutput, err := em.Emit()
	if err != nil {
		fmt.Printf("  %s  %s\n",
			colorize(ansiBoldRed, "✖  EMIT FAILED:"), colorize(ansiRed, err.Error()))
		os.Exit(1)
	}

	fmt.Printf("  %s\n\n", colorize(ansiBold, "AST Node → Docker Service mapping:"))
	for _, entry := range em.Log {
		fmt.Printf("    %s  %s\n", colorize(ansiCyan, "▸"), colorize(ansiWhite, entry))
	}
	fmt.Println()

	if err := em.WriteFile(outputFile); err != nil {
		fmt.Printf("  %s  %s\n",
			colorize(ansiBoldRed, "✖  WRITE FAILED:"), colorize(ansiRed, err.Error()))
		os.Exit(1)
	}

	

	// Write secure .env file (mode 0600) -- secrets stay out of docker-compose.yml
	if envErr := em.WriteEnvFile(".env"); envErr != nil {
		fmt.Printf("  [WARN] Could not write .env: %s\n", envErr.Error())
	} else {
		fmt.Printf("  [lock] .env written (secrets secure, mode 0600).\n")
	}

	absOutput, _ := filepath.Abs(outputFile)
	fmt.Printf("  %s  Written → %s\n\n",
		colorize(ansiGreen, "✔"), colorize(ansiCyan, absOutput))

	// ── YAML dump ─────────────────────────────────────────────────────────
	fmt.Printf("%s\n", sep)
	fmt.Printf("  %s\n", colorize(ansiBold, "GENERATED  docker-compose.yml"))
	fmt.Printf("%s\n\n", sep)
	printYAML(yamlOutput)
	fmt.Printf("\n%s\n\n", sep)

	// ═══════════════════════════════════════════════════════════════════
	// PHASE 5 — Universal Orphan Guard
	// Runs BEFORE any cloud provisioning — regardless of which providers
	// are active.  Prevents silent data loss when a resource is renamed.
	// ═══════════════════════════════════════════════════════════════════
	if orphanErr := runOrphanGuard(program, sep, force); orphanErr != nil {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiBoldRed, "✖  ORPHAN GUARD BLOCKED:"),
			colorize(ansiRed, orphanErr.Error()))
		os.Exit(1)
	}

	// ═══════════════════════════════════════════════════════════════════
	// PHASE 5a — Neon Serverless Postgres
	// ═══════════════════════════════════════════════════════════════════
	fmt.Printf("  %s  %s\n\n",
		colorize(ansiBoldYellow, "⑤a"),
		colorize(ansiYellow, "Cloud provisioning — ☁  Neon Serverless Postgres..."))

	var ce *emitter.CloudEmitter
	if neonKey == "" {
		fmt.Printf("  %s  Skipped — NEON_API_KEY not set (local-only mode).\n\n",
			colorize(ansiYellow, "⚠"))
	} else {
		ce = runCloudProvisionResult(program, neonKey, sep, force)
	}


	// ═══════════════════════════════════════════════════════════════════
	// PHASE 5b — AWS RDS
	// Always runs; uses real credentials when AWS_ACCESS_KEY_ID is set,
	// falls back to the realistic simulation framework otherwise.
	// ═══════════════════════════════════════════════════════════════════
	fmt.Printf("  %s  %s\n\n",
		colorize(ansiBoldYellow, "⑤b"),
		colorize(ansiYellow, "Cloud provisioning — 🟠  AWS RDS / EC2 / S3..."))
	aeGlobal := runAWSProvisionResult(program, sep)

	// ═══════════════════════════════════════════════════════════════════
	// PHASE 5c — Supabase
	// Always runs; uses SUPABASE_ACCESS_TOKEN when set, simulation otherwise.
	// ═══════════════════════════════════════════════════════════════════
	fmt.Printf("  %s  %s\n\n",
		colorize(ansiBoldYellow, "⑤c"),
		colorize(ansiYellow, "Cloud provisioning — 📡  Supabase..."))
	se := runSupabaseProvisionResult(program, sep)

	// ═══════════════════════════════════════════════════════════════════
	// PHASE 5d — Auto .env Injection
	// Collect all real connection strings and write sajon.env (mode 0600)
	// ═══════════════════════════════════════════════════════════════════
	fmt.Printf("  %s  %s\n\n",
		colorize(ansiBoldYellow, "⑤d"),
		colorize(ansiYellow, "Auto .env Injection — writing sajon.env..."))

	var neonResults []emitter.CloudResult
	var awsResults  []emitter.AWSResult
	var supResults  []emitter.SupabaseResult
	if ce != nil {
		neonResults = ce.Results
	}
	if aeGlobal != nil {
		awsResults = aeGlobal.Results
	}
	if se != nil {
		supResults = se.Results
	}
	if envErr := emitter.WriteCloudEnvFile(neonResults, supResults, awsResults); envErr != nil {
		fmt.Printf("  %s  Could not write sajon.env: %s\n\n",
			colorize(ansiBoldYellow, "⚠"), colorize(ansiYellow, envErr.Error()))
	} else if len(neonResults)+len(supResults)+len(awsResults) > 0 {
		absEnv, _ := filepath.Abs("sajon.env")
		fmt.Printf("  %s  %s\n",
			colorize(ansiGreen, "✔"),
			colorize(ansiBoldGreen, "sajon.env written — connection strings ready!"))
		fmt.Printf("     %s\n", colorize(ansiDim, absEnv))
		fmt.Printf("     %s\n",
			colorize(ansiCyan, "Node.js: require('dotenv').config({ path: 'sajon.env' })"))
		fmt.Printf("     %s\n\n",
			colorize(ansiCyan, "Docker:  env_file: [sajon.env]"))
	} else {
		fmt.Printf("  %s  No live cloud results — sajon.env not written (simulation mode).\n\n",
			colorize(ansiYellow, "⚠"))
	}

	// ═══════════════════════════════════════════════════════════════════
	// PHASE 6 — Execute (docker compose up -d)
	// ═══════════════════════════════════════════════════════════════════
	fmt.Printf("  %s  %s\n\n",
		colorize(ansiBoldYellow, "⑥"),
		colorize(ansiYellow, "Launching infrastructure with Docker Compose..."))
	runDockerCompose()

	// ── Final success banner ──────────────────────────────────────────────
	elapsed := time.Since(t0).Round(time.Millisecond)
	fmt.Printf("\n%s\n", colorize(ansiBoldGreen, sep2))
	fmt.Printf("\n  %s\n", colorize(ansiBoldGreen, "🚀  Sajon Cloud Compiler  v"+version+" — Success!"))
	fmt.Printf("  %s\n\n",
		colorize(ansiGreen, fmt.Sprintf(
			"   '%s' generated from '%s' in %s.", outputFile, sourceFile, elapsed)))
	fmt.Printf("  %s\n",
		colorize(ansiWhite, "   Zero DevOps configuration. Your cloud is alive."))
	fmt.Printf("  %s\n\n", colorize(ansiCyan, "   Useful commands:"))
	fmt.Printf("     %s  %s\n", colorize(ansiMagenta, "▸"), colorize(ansiWhite, "sajon ci github            — auto-generate GitHub Actions CI/CD"))
	fmt.Printf("     %s  %s\n", colorize(ansiMagenta, "▸"), colorize(ansiWhite, "docker compose ps          — check running containers"))
	fmt.Printf("     %s  %s\n", colorize(ansiMagenta, "▸"), colorize(ansiWhite, "docker compose logs -f     — stream live logs"))
	fmt.Printf("     %s  %s\n", colorize(ansiMagenta, "▸"), colorize(ansiWhite, "docker compose down -v     — teardown + remove volumes"))
	fmt.Printf("\n%s\n\n", colorize(ansiBoldGreen, sep2))
}

// ── runPlan ───────────────────────────────────────────────────────────────────

// runPlan implements the `sajon plan` dry-run command.
// It parses the source file and checks sajon.lock, then prints a clear
// blueprint of every action that `sajon up` WOULD take.
// No cloud API calls are made; no files are written.
func runPlan(sourceFile string, force bool) {
	sep  := strings.Repeat("─", 62)
	sep2 := strings.Repeat("━", 62)

	// ── Banner ────────────────────────────────────────────────────────────
	fmt.Printf("\n%s\n", colorize(ansiBoldCyan, sep2))
	fmt.Printf("  %s\n", colorize(ansiBoldCyan, "SAJON Cloud Compiler  ·  v"+version))
	fmt.Printf("  %s\n", colorize(ansiMagenta, "Dry-Run Plan  ·  No API calls  ·  No file writes"))
	fmt.Printf("%s\n\n", colorize(ansiBoldCyan, sep2))

	absSource, _ := filepath.Abs(sourceFile)
	fmt.Printf("  %s  %s\n", colorize(ansiBoldYellow, "①"), colorize(ansiYellow, "Reading source file..."))
	fmt.Printf("     %s\n\n", colorize(ansiDim, absSource))

	src, err := os.ReadFile(sourceFile)
	if err != nil {
		fmt.Printf("  %s  Cannot open '%s': %s\n\n",
			colorize(ansiBoldRed, "✖  ERROR"),
			colorize(ansiWhite, sourceFile),
			colorize(ansiRed, err.Error()))
		os.Exit(1)
	}
	lines := strings.Count(string(src), "\n")
	fmt.Printf("  %s  Loaded %s  —  %s, %s\n\n",
		colorize(ansiGreen, "✔"),
		colorize(ansiBold, sourceFile),
		colorize(ansiCyan, fmt.Sprintf("%d bytes", len(src))),
		colorize(ansiCyan, fmt.Sprintf("%d lines", lines)))

	// ── Lex ───────────────────────────────────────────────────────────────
	fmt.Printf("  %s  %s\n", colorize(ansiBoldYellow, "②"), colorize(ansiYellow, "Tokenising source..."))
	l := lexer.New(string(src))
	tokenCount := countTokens(string(src))
	fmt.Printf("  %s  %s\n\n",
		colorize(ansiGreen, "✔"),
		colorize(ansiCyan, fmt.Sprintf("%d tokens produced", tokenCount)))

	// ── Parse ─────────────────────────────────────────────────────────────
	fmt.Printf("  %s  %s\n", colorize(ansiBoldYellow, "③"), colorize(ansiYellow, "Running recursive-descent parser..."))
	p := parser.New(l)
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		fmt.Printf("\n  %s\n\n", colorize(ansiBoldRed, "✖  PARSE FAILED — syntax error(s) in "+sourceFile+":"))
		for i, e := range errs {
			fmt.Printf("  %s  %s\n",
				colorize(ansiRed, fmt.Sprintf("[%d]", i+1)),
				colorize(ansiBoldRed, e))
		}
		fmt.Println()
		os.Exit(1)
	}
	fmt.Printf("  %s  %s\n\n",
		colorize(ansiGreen, "✔"),
		colorize(ansiCyan, fmt.Sprintf("AST built — %d top-level statement(s)", len(program.Statements))))

	// ── AST dump with secret redaction ────────────────────────────────────
	// Shows the same REDACTED view as 'sajon up' so operators can verify
	// that no plaintext secrets appear in plan output.
	fmt.Printf("%s\n", sep)
	fmt.Printf("  %s\n", colorize(ansiBold, "AST DUMP"))
	fmt.Printf("%s\n\n", sep)
	printAST(program)
	fmt.Println()

	// ── Unknown provider scan (T02 fix) ───────────────────────────────────
	// Warn about unrecognised providers so the user knows no provisioning
	// will occur for them — mirrors the warning from Emit() in sajon up.
	{
		knownProviders := map[string]bool{
			"postgres": true, "neon": true, "aws": true,
			"supabase": true, "bigquery": true, "": true,
		}
		for _, stmt := range program.Statements {
			rs, ok := stmt.(*parser.ResourceStatement)
			if !ok {
				continue
			}
			provider := ""
			for _, prop := range rs.Properties {
				if prop.Key == "provider" {
					provider = prop.Value
					break
				}
			}
			if !knownProviders[provider] {
				fmt.Printf("  %s  Unknown provider '%s' for resource '%s' -- no provisioning will occur.\n",
					colorize(ansiYellow, "[WARN]"), provider, rs.Name)
			}
		}
	}

	// ── Lock file check ───────────────────────────────────────────────────
	fmt.Printf("  %s  %s\n", colorize(ansiBoldYellow, "④"), colorize(ansiYellow, "Reading state file (sajon.lock)..."))
	lockPath, _ := filepath.Abs(emitter.LockFilePath)
	if _, statErr := os.Stat(emitter.LockFilePath); os.IsNotExist(statErr) {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiYellow, "⚠"),
			colorize(ansiDim, "sajon.lock not found — all resources will be treated as new"))
	} else {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiGreen, "✔"),
			colorize(ansiCyan, "sajon.lock loaded from "+lockPath))
	}

	// ── Plan ──────────────────────────────────────────────────────────────
	fmt.Printf("  %s  %s\n\n", colorize(ansiBoldYellow, "⑤"), colorize(ansiYellow, "Computing deployment blueprint..."))
	pl := emitter.NewPlanner(program)
	if planErr := pl.Plan(); planErr != nil {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiBoldRed, "✖  PLAN FAILED:"),
			colorize(ansiRed, planErr.Error()))
		os.Exit(1)
	}

	// ── Render blueprint ──────────────────────────────────────────────────
	fmt.Printf("%s\n", sep)
	fmt.Printf("  %s\n", colorize(ansiBoldMagenta, "DEPLOYMENT PLAN"))
	fmt.Printf("  %s\n", colorize(ansiDim, "Sajon will perform the following actions if you run 'sajon up':"))
	fmt.Printf("%s\n\n", sep)

	creates     := 0
	noChanges   := 0
	reprovisions := 0

	for _, action := range pl.Actions {
		switch action.Kind {
		case emitter.ActionNoChange:
			noChanges++
			fmt.Printf("  %s  %s %s\n",
				colorize(ansiBoldCyan, "[~]"),
				colorize(ansiBoldCyan, fmt.Sprintf("[%s] '%s'", action.ResourceKind, action.ResourceName)),
				colorize(ansiDim, "is already live. (No changes)"))
			if action.Detail != "" {
				fmt.Printf("       %s\n", colorize(ansiDim, action.Detail))
			}

		case emitter.ActionReprovision:
			reprovisions++
			fmt.Printf("  %s  %s '%s'\n",
				colorize(ansiBoldYellow, "[-]"),
				colorize(ansiBoldYellow, "Will re-provision: "+action.CloudType),
				colorize(ansiWhite, action.ResourceName))
			if action.Detail != "" {
				fmt.Printf("       %s\n", colorize(ansiYellow, action.Detail))
			}

		case emitter.ActionCreate:
			creates++
			// Different display for different resource kinds
			switch action.ResourceKind {
			case "ENV":
				fmt.Printf("  %s  %s\n",
					colorize(ansiBoldGreen, "[+]"),
					colorize(ansiGreen, fmt.Sprintf("Will inject environment configuration: '%s'", action.ResourceName)))
				if action.Detail != "" {
					fmt.Printf("       %s\n", colorize(ansiDim, action.Detail))
				}
			case "ENDPOINT":
				fmt.Printf("  %s  %s\n",
					colorize(ansiBoldGreen, "[+]"),
					colorize(ansiGreen, fmt.Sprintf("Will register HTTP endpoint: %s", action.ResourceName)))
			case "WORKER":
				fmt.Printf("  %s  %s '%s'\n",
					colorize(ansiBoldGreen, "[+]"),
					colorize(ansiGreen, "Will deploy Background Worker:"),
					colorize(ansiWhite, action.ResourceName))
				if action.Detail != "" {
					fmt.Printf("       %s\n", colorize(ansiDim, action.Detail))
				}
			default:
				fmt.Printf("  %s  %s '%s'\n",
					colorize(ansiBoldGreen, "[+]"),
					colorize(ansiGreen, "Will create new "+action.CloudType+":"),
					colorize(ansiWhite, action.ResourceName))
				if action.Detail != "" {
					fmt.Printf("       %s\n", colorize(ansiDim, action.Detail))
				}
			}
		case emitter.ActionOrphan:
			fmt.Printf("  %s  [ORPHAN] '%s'\n",
				colorize(ansiBoldRed, "[!]"), colorize(ansiWhite, action.ResourceName))
			if action.Detail != "" {
				fmt.Printf("       %s\n", colorize(ansiRed, action.Detail))
			}
			fmt.Printf("       %s\n", colorize(ansiDim, "Run 'sajon down' first, or 'sajon up --force' to bypass this guard."))
		}
	}

	// ── Summary ───────────────────────────────────────────────────────────
	fmt.Printf("\n%s\n", sep)
	fmt.Printf("  %s\n\n", colorize(ansiBold, "PLAN SUMMARY"))

	if creates > 0 {
		fmt.Printf("  %s  %s\n",
			colorize(ansiBoldGreen, "[+]"),
			colorize(ansiGreen, fmt.Sprintf("%d resource(s) will be CREATED", creates)))
	}
	if noChanges > 0 {
		fmt.Printf("  %s  %s\n",
			colorize(ansiBoldCyan, "[~]"),
			colorize(ansiCyan, fmt.Sprintf("%d resource(s) already live — NO CHANGES", noChanges)))
	}
	if reprovisions > 0 {
		fmt.Printf("  %s  %s\n",
			colorize(ansiBoldYellow, "[-]"),
			colorize(ansiYellow, fmt.Sprintf("%d resource(s) will be RE-PROVISIONED", reprovisions)))
	}
	if len(pl.Actions) == 0 {
		fmt.Printf("  %s  No resources found in '%s'.\n",
			colorize(ansiYellow, "⚠"), sourceFile)
	}

	fmt.Printf("\n  %s\n",
		colorize(ansiDim, "No changes have been made.  Run 'sajon up' to apply this plan."))
	fmt.Printf("\n%s\n\n", colorize(ansiBoldCyan, sep2))
}

// ── runDown ───────────────────────────────────────────────────────────────────

// runDown implements the `sajon down` teardown command.
// It reads sajon.lock, calls the appropriate cloud DELETE API for each active
// resource, then removes sajon.lock and docker-compose.yml from disk.
// No .saj source file is required — the lock file is the single source of truth.
func runDown(force bool) {
	sep  := strings.Repeat("─", 62)
	sep2 := strings.Repeat("━", 62)

	// ── Banner ────────────────────────────────────────────────────────────
	fmt.Printf("\n%s\n", colorize(ansiBoldRed, sep2))
	fmt.Printf("  %s  %s\n",
		colorize(ansiBoldRed, "SAJON Cloud Compiler  ·  v"+version),
		colorize(ansiRed, "Teardown Mode"))
	fmt.Printf("  %s\n", colorize(ansiRed, "Destroying all active cloud infrastructure tracked in sajon.lock"))
	fmt.Printf("%s\n\n", colorize(ansiBoldRed, sep2))

	// ── NEON_API_KEY ──────────────────────────────────────────────────────
	neonKey := os.Getenv("NEON_API_KEY")
	if neonKey == "" {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiYellow, "⚠"),
			colorize(ansiYellow, "NEON_API_KEY not set — Neon deletes will be simulated, not live."))
	} else {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiGreen, "✔"),
			colorize(ansiGreen, "NEON_API_KEY detected — Neon projects will be permanently deleted."))
	}

	// ── SUPABASE_ACCESS_TOKEN ─────────────────────────────────────────────
	supabaseToken := os.Getenv("SUPABASE_ACCESS_TOKEN")
	if supabaseToken == "" {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiYellow, "⚠"),
			colorize(ansiYellow, "SUPABASE_ACCESS_TOKEN not set — Supabase deletes will be simulated, not live."))
	} else {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiGreen, "✔"),
			colorize(ansiGreen, "SUPABASE_ACCESS_TOKEN detected — Supabase projects will be permanently deleted."))
	}

	// ── Step 1: Check lock file ───────────────────────────────────────────
	fmt.Printf("  %s  %s\n", colorize(ansiBoldYellow, "①"), colorize(ansiYellow, "Reading state file (sajon.lock)..."))

	if _, statErr := os.Stat(emitter.LockFilePath); os.IsNotExist(statErr) {
		fmt.Printf("\n  %s  %s\n\n",
			colorize(ansiBoldCyan, "[ℹ]"),
			colorize(ansiBoldCyan, "No active infrastructure state found. Nothing to destroy."))
		fmt.Printf("%s\n\n", colorize(ansiBoldCyan, sep2))
		return
	}

	lockPath, _ := filepath.Abs(emitter.LockFilePath)
	fmt.Printf("  %s  %s\n\n",
		colorize(ansiGreen, "✔"),
		colorize(ansiCyan, "sajon.lock found at "+lockPath))

	// ── Step 2: Teardown all resources ───────────────────────────────────
	fmt.Printf("  %s  %s\n\n", colorize(ansiBoldYellow, "②"), colorize(ansiYellow, "Destroying cloud resources..."))
	fmt.Printf("%s\n", sep)

	d := emitter.NewDestroyer(neonKey, supabaseToken)
	found, err := d.TeardownAll()
	if err != nil {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiBoldRed, "✖  TEARDOWN ERROR:"),
			colorize(ansiRed, err.Error()))
		os.Exit(1)
	}
	if !found {
		fmt.Printf("\n  %s  %s\n\n",
			colorize(ansiBoldCyan, "[ℹ]"),
			colorize(ansiBoldCyan, "No active infrastructure state found. Nothing to destroy."))
		fmt.Printf("%s\n\n", colorize(ansiBoldCyan, sep2))
		return
	}

	// Print all log lines from the destroyer
	for _, entry := range d.Log {
		if len(entry) > 2 && entry[:2] == "  " {
			fmt.Printf("  %s\n", colorize(ansiDim, entry))
		} else {
			fmt.Printf("  %s\n", colorize(ansiWhite, entry))
		}
	}
	fmt.Println()

	// ── Step 3: Print per-resource results ───────────────────────────────
	fmt.Printf("%s\n", sep)
	fmt.Printf("  %s\n\n", colorize(ansiBold, "TEARDOWN RESULTS"))

	failed := 0
	for _, result := range d.Results {
		switch result.Status {
		case emitter.DestroyOK:
			badge := colorize(ansiBoldGreen, "✓")
			label := colorize(ansiBoldGreen, "Destroyed")
			detail := colorize(ansiDim, result.Message)
			fmt.Printf("  %s  %-14s  %s '%s'\n       %s\n",
				badge, label,
				colorize(ansiWhite, fmt.Sprintf("[%s]", result.Provider)),
				colorize(ansiWhite, result.ResourceName),
				detail)
		case emitter.DestroySkipped:
			fmt.Printf("  %s  %-14s  '%s'  —  %s\n",
				colorize(ansiBoldCyan, "~"),
				colorize(ansiBoldCyan, "Skipped"),
				colorize(ansiWhite, result.ResourceName),
				colorize(ansiDim, result.Message))
		case emitter.DestroyFailed:
			failed++
			fmt.Printf("  %s  %-14s  '%s'  —  %s\n",
				colorize(ansiBoldRed, "✖"),
				colorize(ansiBoldRed, "FAILED"),
				colorize(ansiWhite, result.ResourceName),
				colorize(ansiRed, result.Message))
		}
	}
	fmt.Println()

	if failed > 0 {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiBoldYellow, "⚠"),
			colorize(ansiYellow, fmt.Sprintf("%d resource(s) failed to delete — check the logs above and remove manually.", failed)))
	}

	// ── Step 4: Remove local files ────────────────────────────────────────
	fmt.Printf("  %s  %s\n", colorize(ansiBoldYellow, "③"), colorize(ansiYellow, "Cleaning up local files..."))
	fmt.Println()

	if err := emitter.RemoveLockFile(); err != nil {
		fmt.Printf("  %s  %s\n", colorize(ansiBoldYellow, "⚠"), colorize(ansiYellow, "Could not remove sajon.lock: "+err.Error()))
	} else {
		fmt.Printf("  %s  %s\n",
			colorize(ansiGreen, "✔"),
			colorize(ansiCyan, "Removed: sajon.lock"))
	}

	if err := emitter.RemoveDockerCompose(outputFile); err != nil {
		fmt.Printf("  %s  %s\n", colorize(ansiBoldYellow, "⚠"), colorize(ansiYellow, "Could not remove "+outputFile+": "+err.Error()))
	} else {
		fmt.Printf("  %s  %s\n",
			colorize(ansiGreen, "✔"),
			colorize(ansiCyan, "Removed: "+outputFile))
	}

	// ── Final success banner ──────────────────────────────────────────────
	fmt.Printf("\n%s\n", colorize(ansiBoldGreen, sep2))
	fmt.Printf("\n  %s\n", colorize(ansiBoldGreen, "✓  Teardown complete."))
	fmt.Printf("  %s\n\n",
		colorize(ansiGreen, "   Your cloud space is clean and zero bills will be generated."))
	fmt.Printf("  %s\n",
		colorize(ansiDim, "   Run 'sajon up' to provision fresh infrastructure anytime."))
	fmt.Printf("\n%s\n\n", colorize(ansiBoldGreen, sep2))
}

// ── printLocalModeWarning ─────────────────────────────────────────────────────

// printLocalModeWarning prints the friendly guidance block shown when
// NEON_API_KEY is absent, then continues execution in local-only mode.
func printLocalModeWarning(sep string) {
	fmt.Printf("%s\n", sep)
	fmt.Printf("  %s\n\n", colorize(ansiBoldYellow, "⚠️  NEON_API_KEY environment variable not found."))
	fmt.Printf("  %s\n",
		colorize(ansiWhite, "  Running in local-only mode."))
	fmt.Printf("  %s\n\n",
		colorize(ansiDim, "  Cloud provisioning (Phase ⑤) will be skipped."))
	fmt.Printf("  To deploy a real Neon Serverless Postgres database, set your key:\n\n")
	fmt.Printf("    %s  %s\n",
		colorize(ansiCyan, "Linux / macOS:"),
		colorize(ansiBold, "export NEON_API_KEY='your_key'"))
	fmt.Printf("    %s  %s\n\n",
		colorize(ansiCyan, "Windows PS:   "),
		colorize(ansiBold, "$env:NEON_API_KEY='your_key'"))
	fmt.Printf("  Get your free API key at: %s\n",
		colorize(ansiBoldCyan, "https://console.neon.tech/app/settings/api-keys"))
	fmt.Printf("%s\n\n", sep)
}

// ── runOrphanGuard ────────────────────────────────────────────────────────────

// runOrphanGuard checks sajon.lock for resources that are active but no longer
// present in the current AST (orphans).  This is a universal check that runs
// before ALL cloud provisioning phases, regardless of provider.
//
// If orphans are found and force==false → returns an error (caller exits 1).
// If orphans are found and force==true  → prints a warning and returns nil.
// If no orphans are found               → returns nil silently.
func runOrphanGuard(program *parser.Program, sep string, force bool) error {
	lf, err := emitter.ReadLockFile()
	if err != nil {
		// Lock read failure is non-fatal here — let the provisioner handle it.
		return nil
	}
	if len(lf.Resources) == 0 {
		return nil // empty lock → nothing to check
	}

	// Build the set of resource names currently in the AST.
	currentNames := make(map[string]bool)
	for _, stmt := range program.Statements {
		if rs, ok := stmt.(*parser.ResourceStatement); ok {
			currentNames[rs.Name] = true
		}
	}

	// Find orphans: active lock entries whose name is gone from the AST.
	var orphans []string
	for name, lr := range lf.Resources {
		if lr.Status == "active" && !currentNames[name] {
			orphans = append(orphans, fmt.Sprintf(
				"  ⚠️  '%s'  (provider: %s  project_id: %s)\n"+
					"       This resource is in sajon.lock but NOT in your .saj file.\n"+
					"       It may have been renamed.  Run 'sajon down' first to safely\n"+
					"       decommission it, or pass --force to override this guard.",
				name, lr.Provider, lr.ProjectID))
		}
	}

	if len(orphans) == 0 {
		return nil
	}

	fmt.Printf("%s\n", sep)
	fmt.Printf("  %s\n\n", colorize(ansiBoldRed, "⛔  ORPHAN GUARD — Rename / Deletion Risk Detected"))
	for _, o := range orphans {
		fmt.Printf("%s\n\n", colorize(ansiRed, o))
	}
	fmt.Printf("%s\n\n", sep)

	if force {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiBoldYellow, "[⚠️  FORCED]"),
			colorize(ansiYellow, fmt.Sprintf("Bypassing orphan guard — proceeding despite %d orphaned resource(s).", len(orphans))))
		return nil
	}

	return fmt.Errorf("%d orphaned resource(s) detected — run 'sajon down' first or use 'sajon up --force' to bypass", len(orphans))
}

// ── runCloudProvision ─────────────────────────────────────────────────────────

// runAWSProvision runs the AWSEmitter against the parsed program.
// AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY are read from the environment;
// if absent the provisioner falls back to the realistic simulation framework.
func runAWSProvision(program *parser.Program, sep string) {
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	defaultRegion := os.Getenv("AWS_DEFAULT_REGION")
	if defaultRegion == "" {
		defaultRegion = "us-east-1"
	}

	mode := "simulation"
	if accessKey != "" && secretKey != "" {
		mode = "live"
	}

	fmt.Printf("  %s  AWS mode: %s\n\n",
		colorize(ansiCyan, "i"),
		colorize(ansiBold, mode+" (set AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY to go live)"))

	ae := emitter.NewAWS(program, accessKey, secretKey, defaultRegion)
	if err := ae.ProvisionAll(); err != nil {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiBoldRed, "✖  AWS PROVISION FAILED:"),
			colorize(ansiRed, err.Error()))
		return
	}

	if len(ae.Results) == 0 {
		fmt.Printf("  %s  No AWS resources in AST — skipped.\n\n",
			colorize(ansiYellow, "⚠"))
		return
	}

	printAWSResults(ae, sep)
}

// printAWSResults renders provisioned AWS resource details to the terminal.
// Output layout varies by ServiceType (RDS / EC2 / S3).
func printAWSResults(ae *emitter.AWSEmitter, sep string) {
	fmt.Printf("%s\n", sep)
	fmt.Printf("  %s\n", colorize(ansiBoldGreen, "🟠  AWS Resources Provisioned"))
	fmt.Printf("%s\n\n", sep)

	for _, entry := range ae.Log {
		if len(entry) > 2 && entry[:2] == "  " {
			fmt.Printf("  %s\n", colorize(ansiDim, entry))
		} else {
			fmt.Printf("  %s\n", colorize(ansiWhite, entry))
		}
	}
	fmt.Println()

	for _, result := range ae.Results {
		switch result.ServiceType {
		case "EC2":
			fmt.Printf("  %s  %s\n",
				colorize(ansiBoldGreen, "✓"),
				colorize(ansiBoldGreen, "AWS EC2 Instance Running!"))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Resource    :"), colorize(ansiWhite, result.ResourceName))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Instance ID :"), colorize(ansiWhite, result.EC2InstanceID))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Type        :"), colorize(ansiWhite, result.InstanceType))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "AMI         :"), colorize(ansiWhite, result.AMI))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Region      :"), colorize(ansiWhite, result.Region))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "VPC         :"), colorize(ansiWhite, result.VPCID))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Security Grp:"), colorize(ansiWhite, result.SecurityGroupID))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Public IP   :"), colorize(ansiBoldGreen, result.PublicIP))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Public DNS  :"), colorize(ansiWhite, result.PublicDNS))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "ARN         :"), colorize(ansiDim, result.ARN))
			fmt.Printf("     %s  %s\n\n",
				colorize(ansiCyan, "SSH         :"),
				colorize(ansiBoldGreen, fmt.Sprintf("ssh -i %s.pem ec2-user@%s", result.KeyPairName, result.PublicIP)))

		case "S3":
			fmt.Printf("  %s  %s\n",
				colorize(ansiBoldGreen, "✓"),
				colorize(ansiBoldGreen, "AWS S3 Bucket Ready!"))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Resource    :"), colorize(ansiWhite, result.ResourceName))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Bucket Name :"), colorize(ansiWhite, result.BucketName))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Region      :"), colorize(ansiWhite, result.Region))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Encryption  :"), colorize(ansiWhite, "SSE-S3 (AES-256)"))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Versioning  :"), colorize(ansiWhite, "Enabled"))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Bucket URL  :"), colorize(ansiBoldGreen, result.BucketURL))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Bucket ARN  :"), colorize(ansiDim, result.BucketARN))
			fmt.Printf("     %s  %s\n\n",
				colorize(ansiCyan, "AWS CLI     :"),
				colorize(ansiBoldGreen, fmt.Sprintf("aws s3 ls s3://%s", result.BucketName)))

		default: // RDS
			fmt.Printf("  %s  %s\n",
				colorize(ansiBoldGreen, "✓"),
				colorize(ansiBoldGreen, "AWS RDS Instance Ready!"))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Resource    :"), colorize(ansiWhite, result.ResourceName))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Instance ID :"), colorize(ansiWhite, result.InstanceID))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Engine      :"), colorize(ansiWhite, result.Engine+" "+result.EngineVersion))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Class       :"), colorize(ansiWhite, result.InstanceClass))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Region      :"), colorize(ansiWhite, result.Region))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "VPC         :"), colorize(ansiWhite, result.VPCID))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Security Grp:"), colorize(ansiWhite, result.SecurityGroupID))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Host        :"), colorize(ansiWhite, result.Host))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Database    :"), colorize(ansiWhite, result.Database))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "User        :"), colorize(ansiWhite, result.User))
			fmt.Printf("     %s  %s\n\n",
				colorize(ansiCyan, "Connection  :"),
				colorize(ansiBoldGreen, result.ConnectionString))
		}
	}
}

// runSupabaseProvision runs the SupabaseEmitter against the parsed program.
// SUPABASE_ACCESS_TOKEN and SUPABASE_ORG_ID are read from the environment;
// if absent the provisioner runs in the realistic simulation framework.
func runSupabaseProvision(program *parser.Program, sep string) {
	accessToken := os.Getenv("SUPABASE_ACCESS_TOKEN")
	orgID       := os.Getenv("SUPABASE_ORG_ID")

	mode := "simulation"
	if accessToken != "" {
		mode = "live"
	}

	// NOTE: mode is printed as plain text so log-capture tools and test
	// scripts can reliably match "Supabase mode: live" without fighting
	// embedded ANSI colour codes (T21 fix).
	fmt.Printf("  %s  Supabase mode: %s  %s\n\n",
		colorize(ansiCyan, "i"),
		mode,
		colorize(ansiDim, "(set SUPABASE_ACCESS_TOKEN to go live)"))

	se := emitter.NewSupabase(program, accessToken, orgID)
	if err := se.ProvisionAll(); err != nil {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiBoldRed, "✖  SUPABASE PROVISION FAILED:"),
			colorize(ansiRed, err.Error()))
		return
	}

	if len(se.Results) == 0 {
		fmt.Printf("  %s  No Supabase resources in AST — skipped.\n\n",
			colorize(ansiYellow, "⚠"))
		return
	}

	printSupabaseResults(se, sep)
}

// printSupabaseResults renders provisioned Supabase project details to the terminal.
func printSupabaseResults(se *emitter.SupabaseEmitter, sep string) {
	fmt.Printf("%s\n", sep)
	fmt.Printf("  %s\n", colorize(ansiBoldGreen, "📡  Supabase Projects Provisioned"))
	fmt.Printf("%s\n\n", sep)

	for _, entry := range se.Log {
		if len(entry) > 2 && entry[:2] == "  " {
			fmt.Printf("  %s\n", colorize(ansiDim, entry))
		} else {
			fmt.Printf("  %s\n", colorize(ansiWhite, entry))
		}
	}
	fmt.Println()

	for _, result := range se.Results {
		fmt.Printf("  %s  %s\n",
			colorize(ansiBoldGreen, "✓"),
			colorize(ansiBoldGreen, "Supabase Project is Live!"))
		fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Resource    :"), colorize(ansiWhite, result.ResourceName))
		fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Project Ref :"), colorize(ansiWhite, result.ProjectRef))
		fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Region      :"), colorize(ansiWhite, result.Region))
		fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Host        :"), colorize(ansiWhite, result.Host))
		fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Pooler Host :"), colorize(ansiWhite, result.PoolerHost))
		fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Database    :"), colorize(ansiWhite, result.Database))
		fmt.Printf("     %s  %s\n", colorize(ansiCyan, "User        :"), colorize(ansiWhite, result.User))
		fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Dashboard   :"), colorize(ansiCyan, result.DashboardURL))
		fmt.Printf("     %s  %s\n\n",
			colorize(ansiCyan, "Connection  :"),
			colorize(ansiBoldGreen, result.ConnectionString))
	}
}

// runCloudProvision instantiates a CloudEmitter, calls ProvisionAll, and
// pretty-prints each live connection string produced by the Neon API.
func runCloudProvision(program *parser.Program, apiKey, sep string, force bool) {
	// NEON_ORG_ID can be set explicitly; if absent the emitter uses the
	// account default discovered during initialisation.
	orgID := os.Getenv("NEON_ORG_ID")
	ce := emitter.NewCloud(program, apiKey, orgID)
	ce.Force = force
	if err := ce.ProvisionAll(); err != nil {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiBoldRed, "✖  CLOUD PROVISION FAILED:"),
			colorize(ansiRed, err.Error()))
		return
	}

	for _, entry := range ce.Log {
		if len(entry) > 2 && entry[:2] == "  " {
			fmt.Printf("  %s\n", colorize(ansiDim, entry))
		} else {
			fmt.Printf("  %s\n", colorize(ansiWhite, entry))
		}
	}
	if len(ce.Log) > 0 {
		fmt.Println()
	}

	if len(ce.Results) == 0 {
		fmt.Printf("  %s  No postgres resources to provision.\n\n",
			colorize(ansiYellow, "⚠"))
		return
	}

	fmt.Printf("%s\n", sep)
	fmt.Printf("  %s\n", colorize(ansiBoldGreen, "☁  Live Cloud Databases Provisioned"))
	fmt.Printf("%s\n\n", sep)

	for _, result := range ce.Results {
		if result.FromCache {
			// Resource was loaded from sajon.lock — no new API call was made.
			fmt.Printf("  %s  %s\n",
				colorize(ansiBoldCyan, "ℹ"),
				colorize(ansiBoldCyan, "Cloud Database Restored from Lock (sajon.lock)"))
			fmt.Printf("     %s  %s\n",
				colorize(ansiCyan, "Resource  :"), colorize(ansiWhite, result.ResourceName))
			fmt.Printf("     %s  %s\n",
				colorize(ansiCyan, "Project ID:"), colorize(ansiWhite, result.ProjectID))
			fmt.Printf("     %s  %s\n",
				colorize(ansiCyan, "Region    :"), colorize(ansiWhite, result.Region))
			fmt.Printf("     %s  %s\n",
				colorize(ansiCyan, "Host      :"), colorize(ansiWhite, result.Host))
			fmt.Printf("     %s  %s\n",
				colorize(ansiCyan, "Database  :"), colorize(ansiWhite, result.Database))
			fmt.Printf("     %s  %s\n",
				colorize(ansiCyan, "User      :"), colorize(ansiWhite, result.User))
			fmt.Printf("     %s  %s\n",
				colorize(ansiCyan, "Dashboard :"),
				colorize(ansiCyan, fmt.Sprintf("https://console.neon.tech/app/projects/%s", result.ProjectID)))
			fmt.Printf("     %s  %s\n\n",
				colorize(ansiCyan, "Connection String:"),
				colorize(ansiBoldGreen, result.ConnectionString))
		} else {
			// Freshly provisioned via the Neon API.
			fmt.Printf("  %s  %s\n",
				colorize(ansiBoldGreen, "✓"),
				colorize(ansiBoldGreen, "Real Cloud Database Created!"))
			fmt.Printf("     %s  %s\n",
				colorize(ansiCyan, "Resource  :"), colorize(ansiWhite, result.ResourceName))
			fmt.Printf("     %s  %s\n",
				colorize(ansiCyan, "Project ID:"), colorize(ansiWhite, result.ProjectID))
			fmt.Printf("     %s  %s\n",
				colorize(ansiCyan, "Region    :"), colorize(ansiWhite, result.Region))
			fmt.Printf("     %s  %s\n",
				colorize(ansiCyan, "Host      :"), colorize(ansiWhite, result.Host))
			fmt.Printf("     %s  %s\n",
				colorize(ansiCyan, "Database  :"), colorize(ansiWhite, result.Database))
			fmt.Printf("     %s  %s\n",
				colorize(ansiCyan, "User      :"), colorize(ansiWhite, result.User))
			fmt.Printf("     %s  %s\n",
				colorize(ansiCyan, "Dashboard :"),
				colorize(ansiCyan, fmt.Sprintf("https://console.neon.tech/app/projects/%s", result.ProjectID)))
			fmt.Printf("     %s  %s\n\n",
				colorize(ansiCyan, "Connection String:"),
				colorize(ansiBoldGreen, result.ConnectionString))
		}
	}
}

// ── runDockerCompose ──────────────────────────────────────────────────────────

func runDockerCompose() {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		printDockerMissingInstructions()
		return
	}
	fmt.Printf("  %s  Docker found at %s\n\n",
		colorize(ansiGreen, "✔"), colorize(ansiDim, dockerPath))

	infoCmd := exec.Command("docker", "info")
	infoCmd.Stdout = nil
	infoCmd.Stderr = nil
	if infoErr := infoCmd.Run(); infoErr != nil {
		fmt.Printf("  %s  Docker daemon is not running.\n", colorize(ansiBoldRed, "✖"))
		fmt.Printf("  %s  Start Docker Desktop then run:\n\n       %s\n\n",
			colorize(ansiCyan, "→"), colorize(ansiBold, "docker compose up -d"))
		return
	}

	fmt.Printf("  %s  Executing: %s\n\n",
		colorize(ansiYellow, "▶"), colorize(ansiBold, "docker compose up -d"))
	upCmd := exec.Command("docker", "compose", "up", "-d")
	upCmd.Stdout = os.Stdout
	upCmd.Stderr = os.Stderr
	if runErr := upCmd.Run(); runErr != nil {
		fmt.Printf("\n  %s  docker compose exited with error: %s\n",
			colorize(ansiBoldRed, "✖"), colorize(ansiRed, runErr.Error()))
		return
	}
	fmt.Printf("\n  %s  All containers started successfully.\n",
		colorize(ansiBoldGreen, "✔"))
}

func printDockerMissingInstructions() {
	sep := strings.Repeat("─", 62)
	fmt.Printf("%s\n  %s  Docker not found on PATH.\n\n", sep, colorize(ansiBoldYellow, "⚠  NOTE"))
	fmt.Printf("  %s has been generated. To provision locally:\n\n", outputFile)
	fmt.Printf("    %s\n\n", colorize(ansiBold+ansiGreen, "docker compose up -d"))
	fmt.Printf("  Install Docker Desktop: %s\n%s\n",
		colorize(ansiCyan, "https://docs.docker.com/get-docker/"), sep)
}

// ── Token counter ─────────────────────────────────────────────────────────────

func countTokens(src string) int {
	l := lexer.New(src)
	count := 0
	for {
		tok := l.NextToken()
		if tok.Type == lexer.TokenEOF {
			break
		}
		count++
	}
	return count
}

// ── AST pretty-printer ────────────────────────────────────────────────────────

// printAST renders the full AST including ResourceStatements, EndpointStatements,
// and EnvStatements. BUG #11 FIX: EnvStatement was previously silently omitted.
func printAST(program *parser.Program) {
	total := len(program.Statements)
	for i, stmt := range program.Statements {
		isLast   := (i == total-1)
		conn     := "├──"
		childPfx := "│  "
		if isLast {
			conn, childPfx = "└──", "   "
		}
		switch node := stmt.(type) {
		case *parser.ResourceStatement:
			printResourceNode(node, conn, childPfx)
		case *parser.EndpointStatement:
			printEndpointNode(node, conn, childPfx)
		case *parser.EnvStatement:
			printEnvNode(node, conn, childPfx)
		}
	}
}

func printResourceNode(rs *parser.ResourceStatement, conn, childPfx string) {
	kc := ansiCyan
	switch rs.Kind {
	case "DATABASE":
		kc = ansiMagenta
	case "WORKER":
		kc = ansiYellow
	}
	fmt.Printf("  %s %s %s\n", conn,
		colorize(kc+ansiBold, fmt.Sprintf("[%s]", rs.Kind)),
		colorize(ansiWhite, rs.Name))

	// Decide which connector to use for the last child (properties OR schema).
	lastChildIsSchema := rs.Schema != nil
	propCount := len(rs.Properties)

	for j, prop := range rs.Properties {
		isLastProp := (j == propCount-1) && !lastChildIsSchema
		pc := "├─"
		if isLastProp {
			pc = "└─"
		}
		fmt.Printf("  %s   %s %s : %s\n", childPfx, pc,
			colorize(ansiCyan, prop.Key),
			colorize(ansiGreen, `"`+prop.Value+`"`))
	}

	// Render the SCHEMA block as a sub-tree inside the resource node.
	if rs.Schema != nil {
		fmt.Printf("  %s   %s %s  %s\n", childPfx, "└─",
			colorize(ansiBold+ansiMagenta, "SCHEMA"),
			colorize(ansiDim, fmt.Sprintf("→ table: %s", rs.Schema.Table)))
		for k, field := range rs.Schema.Fields {
			fc := "├─"
			if k == len(rs.Schema.Fields)-1 {
				fc = "└─"
			}
			fmt.Printf("  %s       %s %s\n", childPfx, fc,
				colorize(ansiGreen, `"`+field+`"`))
		}
		// Show the compiled SQL so the developer knows exactly what will execute.
		compiledSQL := emitter.CompileSchema(rs.Schema)
		if compiledSQL != "" {
			fmt.Printf("  %s       %s %s\n", childPfx, "⚡",
				colorize(ansiYellow, compiledSQL))
		}
	}
}

func printEndpointNode(es *parser.EndpointStatement, conn, childPfx string) {
	fmt.Printf("  %s %s %s %s\n", conn,
		colorize(ansiBold+ansiYellow, "[ENDPOINT]"),
		colorize(ansiBoldRed, es.Method),
		colorize(ansiGreen, `"`+es.Path+`"`))
	for j, bodyStmt := range es.Body {
		bc := "├─"
		if j == len(es.Body)-1 {
			bc = "└─"
		}
		if ret, ok := bodyStmt.(*parser.ReturnStatement); ok {
			fmt.Printf("  %s   %s %s %s\n", childPfx, bc,
				colorize(ansiBoldCyan, "RETURN"),
				colorize(ansiGreen, `"`+ret.Value+`"`))
		}
	}
}

// printEnvNode renders an ENV block in the AST dump.
// Sensitive keys (PASSWORD, SECRET, TOKEN, etc.) are displayed as ***REDACTED***
// to prevent credential exposure in terminal output.
func printEnvNode(es *parser.EnvStatement, conn, childPfx string) {
	fmt.Printf("  %s %s %s\n", conn,
		colorize(ansiBold+ansiGreen, "[ENV]"),
		colorize(ansiWhite, es.Name))
	for j, v := range es.Vars {
		vc := "|-"
		if j == len(es.Vars)-1 {
			vc = "\\-"
		}
		displayVal := v.Value
		if emitter.IsSecretKeyPublic(v.Key) {
			displayVal = "***REDACTED***"
		}
		fmt.Printf("  %s   %s %s : %s\n", childPfx, vc,
			colorize(ansiCyan, v.Key),
			colorize(ansiGreen, `"`+displayVal+`"`))
	}
}


// ── YAML syntax highlighter ───────────────────────────────────────────────────

func printYAML(yaml string) {
	for _, line := range strings.Split(yaml, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "#"):
			fmt.Printf("  %s\n", colorize(ansiDim+ansiCyan, line))
		case strings.Contains(line, ":") && !strings.HasPrefix(trimmed, "-"):
			idx := strings.Index(line, ":")
			fmt.Printf("  %s%s\n",
				colorize(ansiYellow, line[:idx+1]),
				colorize(ansiGreen, line[idx+1:]))
		case strings.HasPrefix(trimmed, "-"):
			fmt.Printf("  %s\n", colorize(ansiMagenta, line))
		default:
			fmt.Printf("  %s\n", colorize(ansiWhite, line))
		}
	}
}

// ── Result-returning cloud provision helpers ──────────────────────────────────
//
// These mirror runCloudProvision / runAWSProvision / runSupabaseProvision but
// return the emitter struct so runPipeline can collect Results for env injection.

// runCloudProvisionResult runs the Neon CloudEmitter and returns it (with Results).
func runCloudProvisionResult(program *parser.Program, apiKey, sep string, force bool) *emitter.CloudEmitter {
	orgID := os.Getenv("NEON_ORG_ID")
	ce := emitter.NewCloud(program, apiKey, orgID)
	ce.Force = force
	if err := ce.ProvisionAll(); err != nil {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiBoldRed, "✖  CLOUD PROVISION FAILED:"),
			colorize(ansiRed, err.Error()))
		return ce
	}
	for _, entry := range ce.Log {
		if len(entry) > 2 && entry[:2] == "  " {
			fmt.Printf("  %s\n", colorize(ansiDim, entry))
		} else {
			fmt.Printf("  %s\n", colorize(ansiWhite, entry))
		}
	}
	if len(ce.Log) > 0 {
		fmt.Println()
	}
	if len(ce.Results) == 0 {
		fmt.Printf("  %s  No postgres resources to provision.\n\n",
			colorize(ansiYellow, "⚠"))
		return ce
	}
	fmt.Printf("%s\n", sep)
	fmt.Printf("  %s\n", colorize(ansiBoldGreen, "☁  Live Cloud Databases Provisioned"))
	fmt.Printf("%s\n\n", sep)
	for _, result := range ce.Results {
		if result.FromCache {
			fmt.Printf("  %s  %s\n", colorize(ansiBoldCyan, "ℹ"), colorize(ansiBoldCyan, "Cloud Database Restored from Lock (sajon.lock)"))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Resource  :"), colorize(ansiWhite, result.ResourceName))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Project ID:"), colorize(ansiWhite, result.ProjectID))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Host      :"), colorize(ansiWhite, result.Host))
			fmt.Printf("     %s  %s\n\n", colorize(ansiCyan, "Connection:"), colorize(ansiBoldGreen, result.ConnectionString))
		} else {
			fmt.Printf("  %s  %s\n", colorize(ansiBoldGreen, "✓"), colorize(ansiBoldGreen, "Real Cloud Database Created!"))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Resource  :"), colorize(ansiWhite, result.ResourceName))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Project ID:"), colorize(ansiWhite, result.ProjectID))
			fmt.Printf("     %s  %s\n", colorize(ansiCyan, "Host      :"), colorize(ansiWhite, result.Host))
			fmt.Printf("     %s  %s\n\n", colorize(ansiCyan, "Connection:"), colorize(ansiBoldGreen, result.ConnectionString))
		}
	}
	return ce
}

// runAWSProvisionResult runs the AWSEmitter and returns it (with Results).
func runAWSProvisionResult(program *parser.Program, sep string) *emitter.AWSEmitter {
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	defaultRegion := os.Getenv("AWS_DEFAULT_REGION")
	if defaultRegion == "" {
		defaultRegion = "us-east-1"
	}
	mode := "simulation"
	if accessKey != "" && secretKey != "" {
		mode = "live"
	}
	fmt.Printf("  %s  AWS mode: %s\n\n",
		colorize(ansiCyan, "i"),
		colorize(ansiBold, mode+" (set AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY to go live)"))
	ae := emitter.NewAWS(program, accessKey, secretKey, defaultRegion)
	if err := ae.ProvisionAll(); err != nil {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiBoldRed, "✖  AWS PROVISION FAILED:"),
			colorize(ansiRed, err.Error()))
		return ae
	}
	if len(ae.Results) == 0 {
		fmt.Printf("  %s  No AWS resources in AST — skipped.\n\n",
			colorize(ansiYellow, "⚠"))
		return ae
	}
	printAWSResults(ae, sep)
	return ae
}

// runSupabaseProvisionResult runs the SupabaseEmitter and returns it (with Results).
func runSupabaseProvisionResult(program *parser.Program, sep string) *emitter.SupabaseEmitter {
	accessToken := os.Getenv("SUPABASE_ACCESS_TOKEN")
	orgID := os.Getenv("SUPABASE_ORG_ID")
	mode := "simulation"
	if accessToken != "" {
		mode = "live"
	}
	fmt.Printf("  %s  Supabase mode: %s  %s\n\n",
		colorize(ansiCyan, "i"),
		mode,
		colorize(ansiDim, "(set SUPABASE_ACCESS_TOKEN to go live)"))
	se := emitter.NewSupabase(program, accessToken, orgID)
	if err := se.ProvisionAll(); err != nil {
		fmt.Printf("  %s  %s\n\n",
			colorize(ansiBoldRed, "✖  SUPABASE PROVISION FAILED:"),
			colorize(ansiRed, err.Error()))
		return se
	}
	if len(se.Results) == 0 {
		fmt.Printf("  %s  No Supabase resources in AST — skipped.\n\n",
			colorize(ansiYellow, "⚠"))
		return se
	}
	printSupabaseResults(se, sep)
	return se
}

// ── runCIInit ─────────────────────────────────────────────────────────────────

// runCIInit implements the 'sajon ci <provider>' command.
// It generates a CI/CD workflow file for the specified provider.
func runCIInit(provider, sajFile string) {
	sep := strings.Repeat("━", 62)
	fmt.Printf("\n%s\n", colorize(ansiBoldMagenta, sep))
	fmt.Printf("  %s  %s\n",
		colorize(ansiBoldMagenta, "SAJON CI Generator  ·  v"+version),
		colorize(ansiWhite, "Zero-config CI/CD pipeline"))
	fmt.Printf("%s\n\n", colorize(ansiBoldMagenta, sep))

	switch provider {
	case "github":
		fmt.Printf("  %s  Generating GitHub Actions workflow...\n\n",
			colorize(ansiCyan, "→"))
		outPath, err := emitter.GenerateCI(emitter.CIGitHub, sajFile)
		if err != nil {
			fmt.Printf("  %s  %s\n\n",
				colorize(ansiBoldRed, "✖  CI GENERATION FAILED:"),
				colorize(ansiRed, err.Error()))
			os.Exit(1)
		}
		absPath, _ := filepath.Abs(outPath)
		fmt.Printf("  %s  %s\n",
			colorize(ansiBoldGreen, "✔"),
			colorize(ansiBoldGreen, "GitHub Actions workflow generated!"))
		fmt.Printf("     %s\n\n", colorize(ansiDim, absPath))
		fmt.Printf("  %s\n\n", colorize(ansiBold, "NEXT STEPS — 3 minutes to full CI/CD:"))
		fmt.Printf("  %s  %s\n",
			colorize(ansiBoldCyan, "1."),
			colorize(ansiWhite, "Push this file to your GitHub repository."))
		fmt.Printf("  %s  %s\n",
			colorize(ansiBoldCyan, "2."),
			colorize(ansiWhite, "Go to: GitHub → Settings → Secrets and variables → Actions"))
		fmt.Printf("  %s  %s\n",
			colorize(ansiBoldCyan, "3."),
			colorize(ansiWhite, "Add these Repository Secrets:"))
		fmt.Printf("\n")
		fmt.Printf("     %s  %s\n",
			colorize(ansiYellow, "NEON_API_KEY          "),
			colorize(ansiDim, "→  from console.neon.tech"))
		fmt.Printf("     %s  %s\n",
			colorize(ansiYellow, "SUPABASE_ACCESS_TOKEN "),
			colorize(ansiDim, "→  from supabase.com/dashboard/account/tokens"))
		fmt.Printf("     %s  %s\n",
			colorize(ansiYellow, "AWS_ACCESS_KEY_ID     "),
			colorize(ansiDim, "→  (optional, only if using AWS)"))
		fmt.Printf("     %s  %s\n",
			colorize(ansiYellow, "AWS_SECRET_ACCESS_KEY "),
			colorize(ansiDim, "→  (optional, only if using AWS)"))
		fmt.Printf("\n")
		fmt.Printf("  %s  %s\n",
			colorize(ansiBoldCyan, "4."),
			colorize(ansiBoldGreen, "Push to main — Sajon auto-deploys your cloud on every commit!"))
		fmt.Printf("\n  %s\n",
			colorize(ansiDim, "  Pull Requests run 'sajon plan' (dry-run, no API calls)."))
		fmt.Printf("  %s\n\n",
			colorize(ansiDim, "  Push to main runs 'sajon up' (live cloud provisioning)."))
		fmt.Printf("%s\n\n", colorize(ansiBoldMagenta, sep))
	default:
		fmt.Printf("  %s  Unknown CI provider: '%s'\n",
			colorize(ansiBoldRed, "✖"), provider)
		fmt.Printf("  %s  Supported providers: %s\n\n",
			colorize(ansiCyan, "→"), colorize(ansiBold, "github"))
		os.Exit(1)
	}
}

