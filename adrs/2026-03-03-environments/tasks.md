---
status: draft
created: 2026-03-03
updated: 2026-03-03
author: adrian
decision: pending
---

# Task List: GitHub Actions Environments Support in Act

This document outlines the tasks required to implement GitHub Actions Environments support in Act, based on the `spec.md` and `plan.md` documents. Each task is designed to be independently implementable with clear acceptance criteria and test requirements.

## Phase 1: Data Model and Workflow Parsing

### Task 1.1: Extend `Job` struct in `pkg/model/workflow.go` ✅

**Description:** Add `RawEnvironment yaml.Node` field to the `Job` struct to capture the raw YAML for the `environment` key.

**Acceptance Criteria:**
- `Job` struct in `pkg/model/workflow.go` contains `RawEnvironment yaml.Node` field with `yaml:"environment"` tag.
- Existing `Job` parsing logic remains unchanged for workflows without an `environment` key.

**Test Requirements:**
- Unit test in `pkg/model/workflow_test.go` to verify `RawEnvironment` field is correctly populated when `environment` key is present in workflow YAML.
- Unit test to ensure no regression for workflows without `environment` key.

**Dependencies:** None
**Effort:** Low (0.5 days)
**Spec Reference:** FR1, Implementation Details (Data Structures)
**Plan Reference:** 2.1
**Status:** ✅ COMPLETED

### Task 1.2: Implement `JobEnvironment` struct and `Environment()` method ✅

**Description:** Create `JobEnvironment` struct and implement the `Environment()` method on the `Job` struct to parse `RawEnvironment` into `JobEnvironment`, handling both string and object formats, and normalizing the environment name to lowercase.

**Acceptance Criteria:**
- `JobEnvironment` struct defined with `Name` (string) and `URL` (string) fields.
- `Environment()` method on `Job` struct correctly parses `environment: production` (string format) into `JobEnvironment{Name: "production", URL: ""}`.
- `Environment()` method correctly parses `environment: { name: production, url: https://example.com }` (object format) into `JobEnvironment{Name: "production", URL: "https://example.com"}`.
- Environment names are normalized to lowercase (e.g., "Production" becomes "production").
- `Environment()` returns `nil` if `RawEnvironment` is empty or invalid.

**Test Requirements:**
- Unit tests in `pkg/model/workflow_test.go` for:
    - Parsing string format environment.
    - Parsing object format environment.
    - Handling empty/missing `environment` key.
    - Case normalization of environment names.
    - Handling malformed `environment` YAML.

**Dependencies:** Task 1.1
**Effort:** Medium (1 day)
**Spec Reference:** FR1, FR6, Implementation Details (Data Structures)
**Plan Reference:** 2.1
**Status:** ✅ COMPLETED

## Phase 2: CLI Flag Parsing and Configuration Loading

### Task 2.1: Define `Config` and `EnvironmentConfig` structs ✅

**Description:** Add `Environments map[string]*EnvironmentConfig` to the `Config` struct in `pkg/runner/runner.go` and define the `EnvironmentConfig` struct.

**Acceptance Criteria:**
- `Config` struct in `pkg/runner/runner.go` contains `Environments map[string]*EnvironmentConfig`.
- `EnvironmentConfig` struct defined with `Vars map[string]string` and `Secrets map[string]string`.

**Test Requirements:**
- Unit test to verify structs are correctly defined and can be initialized.

**Dependencies:** None
**Effort:** Low (0.5 days)
**Spec Reference:** Implementation Details (Data Structures)
**Plan Reference:** 2.2
**Status:** ✅ COMPLETED

### Task 2.2: Create `pkg/runner/environment_config.go` and `ParseEnvironmentFlag` ✅

**Description:** Create a new file `pkg/runner/environment_config.go` and implement `ParseEnvironmentFlag` to parse CLI flags in `environment:value` format, including environment name normalization and validation.

**Acceptance Criteria:**
- New file `pkg/runner/environment_config.go` created.
- `ParseEnvironmentFlag(flag string) (envName, value string, error)` correctly parses `production:KEY=VALUE` into `("production", "KEY=VALUE", nil)`.
- `ParseEnvironmentFlag` normalizes environment names to lowercase.
- `ParseEnvironmentFlag` returns error for malformed flags (e.g., missing colon, empty environment name).
- Environment name validation (max 255 chars) is performed.

**Test Requirements:**
- Unit tests in `pkg/runner/environment_config_test.go` for:
    - Valid flag formats.
    - Malformed flag formats (missing colon, empty name/value).
    - Environment name normalization.
    - Environment name length validation.

**Dependencies:** None
**Effort:** Medium (1 day)
**Spec Reference:** FR4, FR6, Implementation Details (Configuration Loading)
**Plan Reference:** 2.3
**Status:** ✅ COMPLETED

### Task 2.3: Implement `ParseKeyValue` ✅

**Description:** Implement `ParseKeyValue` in `pkg/runner/environment_config.go` to parse `KEY=VALUE` strings.

**Acceptance Criteria:**
- `ParseKeyValue(kv string) (key, value string, error)` correctly parses `KEY=VALUE` into `("KEY", "VALUE", nil)`.
- `ParseKeyValue` returns error for malformed strings (e.g., missing `=`, empty key).

**Test Requirements:**
- Unit tests in `pkg/runner/environment_config_test.go` for:
    - Valid key-value pairs.
    - Malformed key-value pairs.

**Dependencies:** Task 2.2
**Effort:** Low (0.5 days)
**Spec Reference:** Implementation Details (Configuration Loading)
**Plan Reference:** 2.3
**Status:** ✅ COMPLETED

### Task 2.4: Implement `LoadEnvironmentVarsFromFile` and `LoadEnvironmentSecretsFromFile` ✅

**Description:** Implement `LoadEnvironmentVarsFromFile` and `LoadEnvironmentSecretsFromFile` in `pkg/runner/environment_config.go` to load variables/secrets from files, reusing Act's existing file loading logic.

**Acceptance Criteria:**
- `LoadEnvironmentVarsFromFile(path string)` calls `readEnvs()` (case-sensitive) and returns `map[string]string`.
- `LoadEnvironmentSecretsFromFile(path string)` calls `readEnvsEx()` (case-insensitive) and returns `map[string]string`.
- Both functions support dotenv and YAML formats.
- File loading behavior is consistent with existing `--var-file` and `--secret-file`.

**Test Requirements:**
- Unit tests in `pkg/runner/environment_config_test.go` for:
    - Loading from dotenv files (vars and secrets).
    - Loading from YAML files (vars and secrets).
    - Case sensitivity for vars and secrets.
    - Error handling for non-existent or malformed files.

**Dependencies:** Task 2.2, Task 2.3
**Effort:** Medium (1 day)
**Spec Reference:** FR2, Implementation Details (Configuration Loading)
**Plan Reference:** 2.3
**Status:** ✅ COMPLETED

### Task 2.5: Add CLI Flags to `cmd/root.go`

**Description:** Add the new CLI flags (`--environment-var`, `--environment-var-file`, `--environment-secret`, `--environment-secret-file`) to `cmd/root.go`.

**Acceptance Criteria:**
- Flags are correctly defined using `PersistentFlags().StringArrayVar`.
- Flags are repeatable.
- Help text for new flags is clear and accurate.

**Test Requirements:**
- Manual verification of `act --help` output.
- Integration tests will cover flag functionality.

**Dependencies:** None
**Effort:** Low (0.5 days)
**Spec Reference:** FR4, Implementation Details (CLI Integration)
**Plan Reference:** 2.4

### Task 2.6: Implement `processEnvironmentFlags` in `cmd/root.go`

**Description:** Implement `processEnvironmentFlags` to parse the new CLI flags and populate `config.Environments`.

**Acceptance Criteria:**
- `processEnvironmentFlags` iterates through CLI flags.
- Uses `ParseEnvironmentFlag` and `ParseKeyValue` to extract data.
- Populates `config.Environments` map with environment-specific vars and secrets.
- Handles file loading via `LoadEnvironmentVarsFromFile` and `LoadEnvironmentSecretsFromFile`.
- Errors from flag parsing or file loading are propagated.

**Test Requirements:**
- Unit tests for `processEnvironmentFlags` in `cmd/root_test.go` (if feasible, otherwise covered by integration tests).
- Integration tests will verify `config.Environments` is correctly populated.

**Dependencies:** Task 2.1, Task 2.2, Task 2.3, Task 2.4, Task 2.5
**Effort:** Medium (1.5 days)
**Spec Reference:** FR2, FR4, Implementation Details (CLI Integration)
**Plan Reference:** 2.4

## Phase 3: Context Integration

### Task 3.1: Modify `getWorkflowVars()` in `pkg/runner/expression.go`

**Description:** Update `getWorkflowVars()` in `pkg/runner/expression.go` to retrieve environment-specific variables from `Config.Environments` and merge them with repository-level variables, ensuring environment precedence. 

**Implementation approach:**
- Access job via `rc.Run.Job()`
- Get environment via `job.Environment()` which returns `*JobEnvironment` with `Name` field
- If environment exists and `rc.Config.Environments[envName]` is populated, merge environment vars over repository vars
- If environment exists but not configured, log warning using `common.Logger(ctx).Warnf()`

The external interface of `getWorkflowVars()` (signature) will remain unchanged; only internal logic will be modified. Callers such as `NewExpressionEvaluator` and `NewStepExpressionEvaluator` should not require updates.

**Acceptance Criteria:**
- `getWorkflowVars()` returns a map where environment variables override repository variables with the same key.
- If an environment is referenced but not configured, a warning is logged using `common.Logger(ctx).Warnf()`. This warning will be logged each time an environment is encountered without configuration.
- Environment variables do not appear in the `env` context.

**Test Requirements:**
- Unit tests in `pkg/runner/expression_test.go` for:
    - Correct precedence (environment > repository).
    - Warning message for missing environment configuration.
    - Absence of environment variables in `env` context.
- Integration test: `environment-precedence/`

**Dependencies:** Task 1.2, Task 2.6
**Effort:** Medium (1 day)
**Spec Reference:** FR3, FR5, Implementation Details (Context Integration), Edge Cases (Missing Environment Configuration, Duplicate Keys)
**Plan Reference:** 2.5

### Task 3.2: Modify `getWorkflowSecrets()` in `pkg/runner/expression.go`

**Description:** Update `getWorkflowSecrets()` in `pkg/runner/expression.go` to retrieve environment-specific secrets from `Config.Environments` and merge them with repository-level secrets, ensuring environment precedence.

**Implementation approach:**
- Access job via `rc.Run.Job()`
- Get environment via `job.Environment()` which returns `*JobEnvironment` with `Name` field
- If environment exists and `rc.Config.Environments[envName]` is populated, merge environment secrets over repository secrets
- If environment exists but not configured, log warning using `common.Logger(ctx).Warnf()`

The external interface of `getWorkflowSecrets()` (signature) will remain unchanged; only internal logic will be modified. Callers such as `NewExpressionEvaluator` and `NewStepExpressionEvaluator` should not require updates.

**Acceptance Criteria:**
- `getWorkflowSecrets()` returns a map where environment secrets override repository secrets with the same key.
- If an environment is referenced but not configured, a warning is logged using `common.Logger(ctx).Warnf()`. This warning will be logged each time an environment is encountered without configuration.

**Test Requirements:**
- Unit tests in `pkg/runner/expression_test.go` for:
    - Correct precedence (environment > repository).
    - Warning message for missing environment configuration.
- Integration test: `environment-precedence/`

**Dependencies:** Task 1.2, Task 2.6
**Effort:** Medium (1 day)
**Spec Reference:** FR3, FR5, Implementation Details (Context Integration), Edge Cases (Missing Environment Configuration, Duplicate Keys)
**Plan Reference:** 2.5

### Task 3.3: Implement Secret Masking in `pkg/runner/run_context.go`

**Description:** Call `rc.AddMask(secret)` for each environment secret to ensure they are masked in output. Based on Act's architecture, secrets are masked via the `::add-mask::` workflow command mechanism. Environment secrets should be added to masks after `Config.Environments` is populated.

**Implementation approach:**
- In `processEnvironmentFlags` (after populating `Config.Environments`), iterate through all environment secrets
- Call `rc.AddMask(secret)` for each secret value
- Alternatively, this can be done early in job execution before any output is generated
- Note: Act's existing repository secrets appear to be masked via `::add-mask::` commands prepended to step scripts, so environment secrets should follow the same pattern

**Acceptance Criteria:**
- All environment secrets are registered for masking.
- Environment secrets are correctly masked in workflow output.

**Test Requirements:**
- Unit tests for `AddMask` functionality (if not already covered).
- Integration tests (`environment-simple/`, `environment-precedence/`) to verify secrets are masked in output.
- Manual testing to confirm masking.

**Dependencies:** Task 2.6
**Effort:** Low (0.5 days)
**Spec Reference:** FR5, Non-Functional Requirements, Implementation Details (Context Integration)
**Plan Reference:** 2.6

## Phase 4: Testing

### Task 4.1: Create Unit Tests for `pkg/model/workflow.go` ✅

**Description:** Write unit tests for `Job` struct and `Environment()` method.

**Acceptance Criteria:**
- All test requirements from Task 1.1 and 1.2 are met.

**Dependencies:** Task 1.1, Task 1.2
**Effort:** Low (0.5 days)
**Spec Reference:** Testing Strategy (Unit Tests)
**Plan Reference:** 4 (Unit Tests)
**Status:** ✅ COMPLETED

### Task 4.2: Create Unit Tests for `pkg/runner/environment_config.go` ✅

**Description:** Write unit tests for `ParseEnvironmentFlag`, `ParseKeyValue`, `LoadEnvironmentVarsFromFile`, and `LoadEnvironmentSecretsFromFile`.

**Acceptance Criteria:**
- All test requirements from Task 2.2, 2.3, and 2.4 are met.

**Dependencies:** Task 2.2, Task 2.3, Task 2.4
**Effort:** Medium (1 day)
**Spec Reference:** Testing Strategy (Unit Tests)
**Plan Reference:** 4 (Unit Tests)
**Status:** ✅ COMPLETED

### Task 4.3: Create Unit Tests for `pkg/runner/expression.go`

**Description:** Write unit tests for `getWorkflowVars()` and `getWorkflowSecrets()` to verify precedence and warning behavior.

**Acceptance Criteria:**
- All test requirements from Task 3.1 and 3.2 are met.

**Dependencies:** Task 3.1, Task 3.2
**Effort:** Medium (1 day)
**Spec Reference:** Testing Strategy (Unit Tests)
**Plan Reference:** 4 (Unit Tests)

### Task 4.4: Create Integration Tests

**Description:** Create new integration test directories and workflow files in `pkg/runner/testdata/` to cover various environment scenarios.

**Test structure:**
- Each test directory should contain at least a workflow YAML file (e.g., `push.yml`)
- `event.json` files are optional and only needed for specific event types (most tests can use push event)
- Follow existing Act test patterns in `testdata/`

**Acceptance Criteria:**
- `environment-simple/`: Basic vars/secrets access verified.
- `environment-precedence/`: Environment overrides repository values verified.
- `environment-multi/`: Multiple jobs with different environments verified.
- `environment-missing/`: Warning for non-existent environment verified.
- `environment-file-loading/`: Loading from files verified.

**Test Requirements:**
- Each integration test runs successfully and verifies expected behavior.

**Dependencies:** All previous implementation tasks
**Effort:** High (2 days)
**Spec Reference:** Testing Strategy (Integration Tests)
**Plan Reference:** 4 (Integration Tests)

### Task 4.5: Manual Testing

**Description:** Perform manual testing based on the checklist to ensure all aspects of the feature work as expected.

**Acceptance Criteria:**
- All items in the manual testing checklist are verified.
- Backward compatibility is confirmed.

**Dependencies:** All previous tasks
**Effort:** Low (0.5 days)
**Spec Reference:** Testing Strategy (Manual Testing)
**Plan Reference:** 4 (Manual Testing)

## Phase 5: Documentation

### Task 5.1: Update `README.md`

**Description:** Add a new section to `README.md` detailing environment support, CLI flags, and usage examples.

**Acceptance Criteria:**
- `README.md` clearly explains how to use environment features.
- Examples for `--environment-var`, `--environment-secret`, `--environment-var-file`, `--environment-secret-file` are provided.

**Dependencies:** All previous tasks
**Effort:** Medium (1 day)
**Spec Reference:** Documentation
**Plan Reference:** 7 (Documentation)

### Task 5.2: Document Context Differences

**Description:** Add documentation explaining the differences between `vars`, `secrets`, and `env` contexts in the context of environments.

**Acceptance Criteria:**
- Clear explanation of context differences is added to relevant documentation (e.g., `README.md` or a new `docs/` file).

**Dependencies:** All previous tasks
**Effort:** Low (0.5 days)
**Spec Reference:** Documentation
**Plan Reference:** 7 (Documentation)

### Task 5.3: Create Troubleshooting Guide

**Description:** Create a troubleshooting guide for common environment-related issues.

**Acceptance Criteria:**
- A troubleshooting section is added to the documentation, covering issues like malformed flags, missing environments, and precedence conflicts.

**Dependencies:** All previous tasks
**Effort:** Low (0.5 days)
**Spec Reference:** Documentation
**Plan Reference:** 7 (Documentation)
