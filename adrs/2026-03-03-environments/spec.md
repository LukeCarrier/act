# Specification: GitHub Actions Environments Support in Act

## Overview

Implementation of GitHub Actions environments in Act, supporting environment-specific secrets and variables.

## Goals

1. Parse environment configurations from workflow files
2. Support environment-specific secrets and variables that override repository-level values
3. Provide CLI flags for configuring environment secrets and variables locally
4. Maintain backward compatibility
5. Align with GitHub Actions behavior where practical for local execution

## User Journeys

### Testing Deployment Workflow Locally

Developer has a workflow deploying to production with environment-specific configuration:

```yaml
jobs:
  deploy:
    runs-on: ubuntu-latest
    environment: production
    steps:
      - name: Deploy
        run: |
          echo "Deploying to ${{ vars.DEPLOY_URL }}"
          echo "Using API key: ${{ secrets.API_KEY }}"
```

Developer tests locally with staging credentials using `--environment-var` and `--environment-secret` flags.

### Environment Variable Precedence

Developer verifies environment-level variables correctly override repository-level ones, matching GitHub's behavior.

## Functional Requirements

### FR1: Workflow Parsing

Parse `environment` key from job definitions. Supports two formats:

**Simple string:**
```yaml
environment: production
```

**Object with URL:**
```yaml
environment:
  name: production
  url: https://example.com
```

**Implementation:**
- Add `RawEnvironment yaml.Node` to `Job` struct
- Add `Environment()` method to extract name and URL

### FR2: Environment Configuration via CLI

Support environment-specific secrets and variables through CLI flags with `environment:value` format.

**Examples:**

```bash
# Environment-specific variables (accessible via vars context)
act --environment-var production:DEPLOY_URL=https://prod.example.com \
    --environment-var staging:DEPLOY_URL=https://staging.example.com

# Environment-specific secrets (accessible via secrets context)
act --environment-secret production:API_KEY=prod-key \
    --environment-secret staging:API_KEY=staging-key

# Load from files
act --environment-var-file production:prod.vars \
    --environment-secret-file production:prod.secrets
```

**File Format:**

Uses Act's existing file loading (`readEnvs()` and `readEnvsEx()`):
- Supports dotenv (key=value) and YAML (.yml/.yaml)
- Variables: case-sensitive keys
- Secrets: case-insensitive keys

Example:
```
DEPLOY_URL=https://prod.example.com
LOG_LEVEL=error
```

### FR3: Variable and Secret Resolution

Precedence order:

**Variables (via `vars` context):**
1. Environment-level (highest priority)
2. Repository-level (from `--var` or `--var-file`)

**Secrets (via `secrets` context):**
1. Environment-level (highest priority)
2. Repository-level (from `--secret` or `--secret-file`)

**Important:** Environment variables do NOT affect the `env` context. Only job-level and workflow-level `env` affect this context.

### FR4: CLI Flags

- `--environment-var <environment:key=value>`: Set environment-specific variable
- `--environment-var-file <environment:path>`: Load environment-specific variables from file
- `--environment-secret <environment:key=value>`: Set environment-specific secret
- `--environment-secret-file <environment:path>`: Load environment-specific secrets from file

Format: `environment_name:value` where value is `key=value` or `path`. Multiple flags can be specified for different environments.

### FR5: Context Population

When a job references an environment:

1. Load environment-specific variables into `vars` context
2. Load environment-specific secrets into `secrets` context
3. Environment values override repository-level values
4. Mask environment secrets in output

### FR6: Environment Name Validation

- Case-insensitive (normalized to lowercase)
- Maximum length: 255 characters
- Normalization happens during workflow parsing and CLI flag parsing

## Non-Functional Requirements

- Environment configuration loading should not significantly impact startup time
- Existing workflows without environments continue to work unchanged
- Clear error messages for malformed configuration
- Warning (not error) when job references undefined environment
- Environment secrets masked in output

## Implementation Details

### Data Structures

`pkg/model/workflow.go`:
```go
type Job struct {
    RawEnvironment yaml.Node `yaml:"environment"`
}

type JobEnvironment struct {
    Name string `yaml:"name"`
    URL  string `yaml:"url"`
}
```

`pkg/runner/runner.go`:
```go
type Config struct {
    Environments map[string]*EnvironmentConfig
}

type EnvironmentConfig struct {
    Vars    map[string]string
    Secrets map[string]string
}
```

### Configuration Loading

`pkg/runner/environment_config.go`:
- `ParseEnvironmentFlag(flag string) (envName, value string, error)` - Parse `environment:value`, normalize to lowercase
- `ParseKeyValue(kv string) (key, value string, error)` - Parse `KEY=value`
- `LoadEnvironmentVarsFromFile(path string)` - Call `readEnvs()` (case-sensitive)
- `LoadEnvironmentSecretsFromFile(path string)` - Call `readEnvsEx()` (case-insensitive)

### Context Integration

`pkg/runner/expression.go`:
- Modify `getWorkflowVars()` to merge environment variables (environment > repository)
- Modify `getWorkflowSecrets()` to merge environment secrets (environment > repository)
- Log warning via `common.Logger(ctx).Warnf()` if environment not configured

`pkg/runner/run_context.go`:
- Call `rc.AddMask(secret)` for each environment secret during job initialization

### CLI Integration

`cmd/root.go`:
- Add `--environment-var`, `--environment-var-file`, `--environment-secret`, `--environment-secret-file` flags
- Parse environment configuration during initialization

## Acceptance Criteria

### AC1: Basic Environment Support
- [ ] Act can parse `environment: name` from workflow files
- [ ] Act can parse `environment: { name: name, url: url }` from workflow files
- [ ] No errors when environment is specified but no configuration exists

### AC2: Environment Variables
- [ ] Environment variables are accessible via `vars.VAR_NAME`
- [ ] Environment variables override repository-level variables
- [ ] Environment variables do NOT appear in `env` context

### AC3: Environment Secrets
- [ ] Environment secrets are accessible via `secrets.SECRET_NAME`
- [ ] Environment secrets override repository-level secrets
- [ ] Environment secrets are masked in output

### AC4: CLI Flags
- [ ] `--environment-var environment:KEY=value` sets environment-specific variable
- [ ] `--environment-var-file environment:path` loads variables from file
- [ ] `--environment-secret environment:KEY=value` sets environment-specific secret
- [ ] `--environment-secret-file environment:path` loads secrets from file
- [ ] Malformed flag format produces clear error message
- [ ] Multiple flags can be specified for different environments

### AC5: Backward Compatibility
- [ ] Workflows without environments work unchanged
- [ ] Existing `--var` and `--secret` flags work as before
- [ ] No breaking changes to existing Act behavior

## Edge Cases

### Missing Environment Configuration
Job references `environment: production` but no configuration exists.
- Log warning: "Environment 'production' referenced but not configured"
- Continue execution using repository-level secrets/vars only

### Duplicate Keys
Same variable/secret at repository and environment level.
- Environment value takes precedence

### Empty Environment Name
`environment: ""` treated as no environment specified.

### Case Sensitivity
Configuration has `Production`, workflow references `production`.
- Normalized to lowercase, matches successfully

### Malformed CLI Flag
Invalid format (missing colon, etc.).
- Fail with clear error: "Expected format: environment:key=value or environment:path"

### Environment URL
Job specifies `environment.url`.
- Parse and store without validation
- GitHub Actions does not expose URL in any context
- No functional impact (used for deployment tracking UI only)

## Out of Scope

The following GitHub Actions environment features are not implemented:

1. **Protection Rules** - Required reviewers, wait timers, deployment branch restrictions
2. **Deployment Objects** - Creation of deployment and deployment status objects
3. **Custom Protection Rules** - GitHub Apps-based protection
4. **Deployment History** - Tracking of deployment history and status

Rationale: Act runs locally without GitHub's approval infrastructure or API integration.

## Testing Strategy

### Unit Tests
- Parse environment from workflow YAML (string and object formats)
- Parse CLI flags (`environment:value` format)
- Load variables/secrets from files
- Variable/secret precedence resolution
- Environment name normalization
- CLI flag validation and error handling

### Integration Tests
Test workflows in `pkg/runner/testdata/`:
- `environment-simple/`: Basic environment usage
- `environment-precedence/`: Variable/secret precedence
- `environment-multi/`: Multiple environments in one workflow
- `environment-missing/`: Missing environment handling

## Documentation

1. Update README.md with environment support
2. Document CLI flags and usage examples
3. Explain `vars` vs `secrets` vs `env` context differences
4. Provide troubleshooting guide

## Migration

Existing Act users:
1. No action required if not using environments
2. To use environments:
   - `--environment-var environment:KEY=value` for variables
   - `--environment-secret environment:KEY=value` for secrets
   - Or use `--environment-var-file` and `--environment-secret-file` for bulk configuration
3. Existing `--var` and `--secret` flags continue to work as repository-level configuration

## References

- [GitHub Actions: Using environments for deployment](https://docs.github.com/en/actions/deployment/targeting-different-environments/using-environments-for-deployment)
- [GitHub Actions: Contexts](https://docs.github.com/en/actions/learn-github-actions/contexts)
