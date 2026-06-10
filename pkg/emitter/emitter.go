// Package emitter implements Phase 3 of the Sajon compiler pipeline.
// It traverses the typed AST produced by the parser and generates real
// infrastructure artefacts -- starting with a production-grade docker-compose.yml.
//
// SECURITY -- ENV variable handling:
//   - docker-compose.yml uses ${KEY} substitution references -- NO plaintext secrets.
//   - Actual values are written to .env (mode 0600) via WriteEnvFile().
//   - Terminal logs redact values whose key names suggest sensitivity.
package emitter

import (
	"fmt"
	"os"
	"strings"
	"time"

	"sajon/pkg/parser"
)

// Emitter holds a reference to the root AST node and accumulates both the
// generated YAML and a human-readable emit log for terminal display.
type Emitter struct {
	program *parser.Program
	output  string
	Log     []string
}

// New constructs an Emitter for the given parsed program.
func New(p *parser.Program) *Emitter {
	return &Emitter{program: p}
}

// Emit performs a single-pass traversal of the AST and assembles a complete
// docker-compose.yml string. ENV values use ${KEY} substitution -- call
// WriteEnvFile() to produce the .env file that docker-compose reads at runtime.
func (e *Emitter) Emit() (string, error) {
	var (
		postgresNodes []*parser.ResourceStatement
		bigqueryNodes []*parser.ResourceStatement
		workerNodes   []*parser.ResourceStatement
		endpoints     []*parser.EndpointStatement
		envVars       []parser.Property
	)

	seenResourceNames := map[string]bool{}

	knownProviders := map[string]bool{
		"postgres": true, "neon": true, "aws": true,
		"supabase": true, "bigquery": true, "": true,
	}

	for _, stmt := range e.program.Statements {
		switch node := stmt.(type) {
		case *parser.ResourceStatement:
			if seenResourceNames[node.Name] {
				fmt.Printf("  [WARN] Duplicate resource name '%s' -- only the first definition will be used.\n", node.Name)
				continue
			}
			seenResourceNames[node.Name] = true

			provider := propValue(node, "provider")
			if !knownProviders[provider] {
				fmt.Printf("  [WARN] Unknown provider '%s' for resource '%s' -- no provisioning will occur.\n", provider, node.Name)
			}

			switch node.Kind {
			case "RESOURCE", "DATABASE":
				switch {
				case propValue(node, "provider") == "postgres":
					postgresNodes = append(postgresNodes, node)
				case propValue(node, "engine") == "postgres":
					postgresNodes = append(postgresNodes, node)
				case propValue(node, "provider") == "neon" && propValue(node, "type") == "postgres":
					postgresNodes = append(postgresNodes, node)
				case propValue(node, "engine") == "bigquery":
					bigqueryNodes = append(bigqueryNodes, node)
				}
			case "WORKER":
				workerNodes = append(workerNodes, node)
			case "STORAGE", "SERVER":
				if propValue(node, "provider") == "aws" {
					e.addLog(fmt.Sprintf("[%s] %-18s ->  cloud-managed (AWS) -- no local container generated", node.Kind, node.Name))
				}
			}
		case *parser.EndpointStatement:
			endpoints = append(endpoints, node)
		case *parser.EnvStatement:
			envVars = append(envVars, node.Vars...)
			e.addLog(fmt.Sprintf("[ENV]  %-18s ->  %d var(s) -- values stored in .env (secrets redacted from YAML)", node.Name, len(node.Vars)))
		}
	}

	var sb strings.Builder
	var namedVolumes []string
	var dbServiceNames []string

	sb.WriteString(e.fileHeader())
	sb.WriteString("services:\n")

	for i, node := range postgresNodes {
		version := propValue(node, "version")
		if version == "" {
			version = "15"
		}
		image    := fmt.Sprintf("postgres:%s", version)
		volName  := node.Name + "_data"
		dbURL    := fmt.Sprintf("postgres://sajon:sajon_secret@%s:5432/%s", node.Name, node.Name)
		hostPort := 5432 + i

		namedVolumes   = append(namedVolumes, volName)
		dbServiceNames = append(dbServiceNames, node.Name)

		e.addLog(fmt.Sprintf("[%s] %-18s ->  service %-18s  image: %s  host-port: %d", node.Kind, node.Name, node.Name, image, hostPort))

		if hostPort != 5432 {
			fmt.Printf("  [INFO] Multiple postgres DBs detected -- '%s' mapped to host port %d to avoid conflict.\n", node.Name, hostPort)
		}

		sb.WriteString(fmt.Sprintf("\n  # -- [%s] %s\n", node.Kind, node.Name))
		sb.WriteString(fmt.Sprintf("  %s:\n", node.Name))
		sb.WriteString(fmt.Sprintf("    image: %s\n", image))
		sb.WriteString(fmt.Sprintf("    container_name: %s\n", node.Name))
		sb.WriteString("    environment:\n")
		sb.WriteString(fmt.Sprintf("      POSTGRES_DB:       %s\n", node.Name))
		sb.WriteString("      POSTGRES_USER:     sajon\n")
		sb.WriteString("      POSTGRES_PASSWORD: sajon_secret\n")
		sb.WriteString(fmt.Sprintf("      DATABASE_URL:      \"%s\"\n", dbURL))
		for _, ev := range envVars {
			sb.WriteString(fmt.Sprintf("      %-22s ${%s}\n", ev.Key+":", ev.Key))
		}
		sb.WriteString("    ports:\n")
		sb.WriteString(fmt.Sprintf("      - \"%d:5432\"\n", hostPort))
		sb.WriteString("    volumes:\n")
		sb.WriteString(fmt.Sprintf("      - %s:/var/lib/postgresql/data\n", volName))
		sb.WriteString("    restart: unless-stopped\n")
		sb.WriteString("    healthcheck:\n")
		sb.WriteString("      test: [\"CMD-SHELL\", \"pg_isready -U sajon\"]\n")
		sb.WriteString("      interval: 10s\n")
		sb.WriteString("      timeout: 5s\n")
		sb.WriteString("      retries: 5\n")
	}

	for _, node := range bigqueryNodes {
		dataset := propValue(node, "dataset")
		if dataset == "" {
			dataset = node.Name
		}
		e.addLog(fmt.Sprintf("[%s] %-18s ->  cloud-managed (BigQuery) -- env-var stub injected into api_server", node.Kind, node.Name))
		sb.WriteString(fmt.Sprintf("\n  # -- [%s] %s (BigQuery cloud-managed)\n", node.Kind, node.Name))
		sb.WriteString(fmt.Sprintf("  # Dataset '%s' -- connection config injected via BIGQUERY_PROJECT / BIGQUERY_DATASET.\n", dataset))
	}

	if len(endpoints) > 0 {
		var routeLabels []string
		for _, ep := range endpoints {
			routeLabels = append(routeLabels, fmt.Sprintf("%s %s", ep.Method, ep.Path))
			e.addLog(fmt.Sprintf("[ENDPOINT] %-6s %-14s  ->  route on sajon_api", ep.Method, ep.Path))
			if len(ep.Body) == 0 {
				fmt.Printf("  [WARN] ENDPOINT '%s %s' has no RETURN statement -- attach a runtime to serve this route.\n", ep.Method, ep.Path)
			}
		}

		dbURL := ""
		if len(postgresNodes) > 0 {
			n := postgresNodes[0]
			dbURL = fmt.Sprintf("postgres://sajon:sajon_secret@%s:5432/%s", n.Name, n.Name)
		}

		var bqEnv strings.Builder
		for _, bq := range bigqueryNodes {
			bqEnv.WriteString(fmt.Sprintf("      BIGQUERY_PROJECT:  \"%s\"\n", "your-gcp-project"))
			bqEnv.WriteString(fmt.Sprintf("      BIGQUERY_DATASET:  \"%s\"\n", propValue(bq, "dataset")))
		}

		sb.WriteString("\n  # -- [ENDPOINT] API Gateway (Sajon HTTP Server)\n")
		sb.WriteString("  # Routes registered:\n")
		for _, r := range routeLabels {
			sb.WriteString(fmt.Sprintf("  #   > %s\n", r))
		}
		sb.WriteString("  sajon_api:\n")
		sb.WriteString("    image: node:20-alpine\n")
		sb.WriteString("    container_name: sajon_api\n")
		sb.WriteString("    working_dir: /app\n")
		sb.WriteString("    volumes:\n")
		sb.WriteString("      - ./:/app\n")
		sb.WriteString("    ports:\n")
		sb.WriteString("      - \"3000:3000\"\n")
		sb.WriteString("    environment:\n")
		sb.WriteString("      NODE_ENV: production\n")
		if dbURL != "" {
			sb.WriteString(fmt.Sprintf("      DATABASE_URL:      \"%s\"\n", dbURL))
		}
		if bqEnv.Len() > 0 {
			sb.WriteString(bqEnv.String())
		}
		for _, ev := range envVars {
			sb.WriteString(fmt.Sprintf("      %-22s ${%s}\n", ev.Key+":", ev.Key))
		}
		sb.WriteString("    command: [\"sh\", \"-c\", \"echo 'Sajon API Gateway -- attach your runtime'\"]\n")
		if len(dbServiceNames) > 0 {
			sb.WriteString("    depends_on:\n")
			for _, dep := range dbServiceNames {
				sb.WriteString(fmt.Sprintf("      %s:\n", dep))
				sb.WriteString("        condition: service_healthy\n")
			}
		}
		sb.WriteString("    restart: unless-stopped\n")
	}

	for _, node := range workerNodes {
		queue       := propValue(node, "queue")
		concurrency := propValue(node, "concurrency")
		if concurrency == "" {
			concurrency = "1"
		} else if concurrency == "0" {
			fmt.Printf("  [WARN] Worker '%s' has concurrency=0 -- it will start but process no jobs. Did you mean 1?\n", node.Name)
		}
		if queue == "" {
			queue = "default"
			fmt.Printf("  [WARN] Worker '%s' has no 'queue' property -- defaulting to queue name \"default\".\n", node.Name)
		}
		dbURL := ""
		if len(postgresNodes) > 0 {
			n := postgresNodes[0]
			dbURL = fmt.Sprintf("postgres://sajon:sajon_secret@%s:5432/%s", n.Name, n.Name)
		}

		e.addLog(fmt.Sprintf("[WORKER]   %-18s ->  service %-18s  queue: %s  concurrency: %s", node.Name, node.Name, queue, concurrency))

		sb.WriteString(fmt.Sprintf("\n  # -- [WORKER] %s\n", node.Name))
		sb.WriteString(fmt.Sprintf("  %s:\n", node.Name))
		sb.WriteString("    image: node:20-alpine\n")
		sb.WriteString(fmt.Sprintf("    container_name: %s\n", node.Name))
		sb.WriteString("    working_dir: /app\n")
		sb.WriteString("    volumes:\n")
		sb.WriteString("      - ./:/app\n")
		sb.WriteString("    environment:\n")
		sb.WriteString(fmt.Sprintf("      QUEUE_NAME:   \"%s\"\n", queue))
		sb.WriteString(fmt.Sprintf("      CONCURRENCY:  \"%s\"\n", concurrency))
		if dbURL != "" {
			sb.WriteString(fmt.Sprintf("      DATABASE_URL: \"%s\"\n", dbURL))
		}
		for _, ev := range envVars {
			sb.WriteString(fmt.Sprintf("      %-22s ${%s}\n", ev.Key+":", ev.Key))
		}
		sb.WriteString(fmt.Sprintf("    command: [\"sh\", \"-c\", \"echo 'Sajon Worker started: %s'\"]\n", node.Name))
		if len(endpoints) > 0 {
			sb.WriteString("    depends_on:\n")
			sb.WriteString("      - sajon_api\n")
		}
		sb.WriteString("    restart: unless-stopped\n")
	}

	if len(namedVolumes) > 0 {
		sb.WriteString("\nvolumes:\n")
		for _, vol := range namedVolumes {
			sb.WriteString(fmt.Sprintf("  %s:\n", vol))
			sb.WriteString("    driver: local\n")
		}
	}

	e.output = sb.String()
	return e.output, nil
}

// WriteFile persists the output from the last Emit() call to the given path.
func (e *Emitter) WriteFile(filename string) error {
	if e.output == "" {
		return fmt.Errorf("nothing to write: call Emit() before WriteFile()")
	}
	return os.WriteFile(filename, []byte(e.output), 0644)
}

// WriteEnvFile writes all ENV block key=value pairs to filename (typically ".env")
// so that docker-compose can load them via ${KEY} variable substitution.
// The file is written with mode 0600 (owner-read-only) to keep secrets local.
// Sensitive keys are redacted in terminal log output.
func (e *Emitter) WriteEnvFile(filename string) error {
	var envVars []parser.Property
	for _, stmt := range e.program.Statements {
		if ev, ok := stmt.(*parser.EnvStatement); ok {
			envVars = append(envVars, ev.Vars...)
		}
	}
	if len(envVars) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("# .env -- generated by Sajon Compiler\n")
	sb.WriteString("# Referenced by docker-compose.yml for ${KEY} variable substitution.\n")
	sb.WriteString("# WARNING: Keep this file private -- do NOT commit it to version control.\n\n")

	for _, ev := range envVars {
		sb.WriteString(fmt.Sprintf("%s=%s\n", ev.Key, ev.Value))
		if isSecretKey(ev.Key) {
			e.addLog(fmt.Sprintf("[ENV]  %s=***REDACTED*** (written securely to %s)", ev.Key, filename))
		} else {
			e.addLog(fmt.Sprintf("[ENV]  %s=%s (written to %s)", ev.Key, ev.Value, filename))
		}
	}

	if err := os.WriteFile(filename, []byte(sb.String()), 0600); err != nil {
		return fmt.Errorf("write %s: %w", filename, err)
	}
	return nil
}

// isSecretKey returns true when the key name suggests the value is sensitive.
func isSecretKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, w := range []string{"PASSWORD", "SECRET", "TOKEN", "APIKEY", "API_KEY", "PASS", "CREDENTIAL"} {
		if strings.Contains(upper, w) {
			return true
		}
	}
	return false
}

// IsSecretKeyPublic is the exported form of isSecretKey so main.go can
// redact sensitive ENV values in the AST dump without duplicating the logic.
func IsSecretKeyPublic(key string) bool { return isSecretKey(key) }


// propValue returns the value of the first property matching key, or "".
func propValue(node *parser.ResourceStatement, key string) string {
	for _, p := range node.Properties {
		if p.Key == key {
			return p.Value
		}
	}
	return ""
}

// addLog appends one human-readable mapping event to the emit log.
func (e *Emitter) addLog(msg string) {
	e.Log = append(e.Log, msg)
}

// fileHeader returns the top-of-file comment block stamped with the current date.
func (e *Emitter) fileHeader() string {
	return fmt.Sprintf(`# ================================================================
# Generated by the Sajon Compiler  v1.0.0
# Timestamp : %s
# Source    : .saj cloud-native infrastructure definition
#
# DO NOT EDIT -- this file is auto-generated.
# Modify your .saj source file and re-run the compiler.
#
# SECURITY NOTE: ENV values use ${KEY} substitution.
# Actual values are loaded from the .env file at runtime.
# Never commit .env to version control.
#
# Usage:
#   docker compose up -d          # start all services
#   docker compose down -v        # tear down (remove volumes too)
# ================================================================

version: "3.9"

`, time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))
}