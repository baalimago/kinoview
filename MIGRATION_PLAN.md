# Go Test Style Standards and Migration Plan

## Test Style Standards

### 1. Test Naming Conventions

- **Functions**: `TestFunctionName` (e.g., `TestSetup`, `TestClassify`)
- **Methods**: `TestType_struct_Method` (e.g., `Test_store_Setup`, `Test_indexer_Start`)
- **Subtests**: Use descriptive names in `t.Run("description", func(t *testing.T) {...})`

### 2. Assertion Patterns

- **Primary**: Use `testboil` helpers when available
- Write new generic testing tools if these may be used in more than 3 locations
- **Fallback**: Standard Go patterns with helper functions
- **Error checking**: Consistent `standardErrorCheck` pattern
- **Always use `t.Helper()` in test helper functions**
- **Do not use any third part tools**

### 3. Mock Patterns

- **Location**: Keep mocks in same test file or dedicated `t_test.go` files
- **Naming**: `mock` + `InterfaceName` (e.g., `mockClassifier`, `mockStore`)
- **Structure**: Embed function fields for flexible behavior

### 4. Context Handling

- **Timeouts**: Use `withTestTimeout` helper
- **Cancellation**: Use `testboil.ReturnsOnContextCancel` for cancel testing
- **Cleanup**: Always use `t.Cleanup()` for context cancellation

### 5. Test Structure

- **Setup/Teardown**: Use `t.TempDir()` and `t.Cleanup()`
- **Table-driven**: Use for multiple similar test cases
- **Subtests**: Use `t.Run()` for logical grouping

## Migration Checklist

Each file needs these specific changes:

### `./cmd/serve/serve_test.go`

**Issues**:

- Mixed assertion styles (some use custom patterns, others standard)
- Inconsistent error checking patterns

**Actions**:

- Rename test functions to remove underscores
- Standardize error assertions using helper pattern
- Add `t.Helper()` to any helper functions

### `./internal/agent/classifier_test.go`

**Issues**:

- Inconsistent subtest naming (mix of snake_case and descriptive)
- Mock pattern is good but could be more consistent
- Some assertions use `testboil`, others don't

**Actions**:

- Standardize subtest names to be descriptive
- Ensure all similar assertions use `testboil.FailTestIfDiff`
- Add consistent error checking pattern

### `./internal/media/index_test.go`

**Issues**:

- Function naming: `Test_Indexer_Setup` → `Test_indexer_Setup`
- Mixed assertion styles
- Some subtests have inconsistent naming

**Actions**:

- Rename test functions following method naming convention
- Standardize all assertions to use `testboil` where appropriate
- Make subtest names descriptive rather than technical

### `./internal/media/storage/store_test.go`

**Issues**:

- Very long test functions that could be broken down
- Inconsistent mock setup patterns

**Actions**:

- Rename test functions with proper casing
- Break down long test functions into focused subtests
- Standardize mock setup using consistent pattern
- Use `withTestTimeout` helper for context tests

### `./internal/media/storage/handlers_test.go`

**Issues**:

- Function naming: `Test_jsonStore_ListHandlerFunc` → `TestJSONStore_ListHandlerFunc`
- Inconsistent HTTP test patterns
- Mixed assertion styles

**Actions**:

- Rename test functions with proper casing
- Standardize HTTP test setup using consistent helper pattern
- Use consistent assertion style throughout

### `./internal/media/storage/classification_test.go`

**Issues**:

- Good structure overall but inconsistent helper function usage
- Some custom assertion patterns that could use `testboil`
- Context handling could be more standardized

**Actions**:

- Add `t.Helper()` to `waitUntil` and other helper functions
- Standardize context timeout patterns using helper
- Use consistent assertion style for error checking

### `./internal/media/storage/t_test.go`

**Issues**:

- Good mock patterns but could be more consistent
- Missing some interface implementations

**Actions**:

- Ensure all mocks follow consistent naming pattern
- Add any missing interface method implementations
- Add documentation comments for mock purposes

### `./internal/media/watcher/watcher_recursive_test.go`

**Issues**:

- Inconsistent assertion patterns
- Some complex test logic that could be simplified

**Actions**:

- Rename test functions with proper casing
- Standardize assertion patterns using `testboil`
- Simplify complex test setup where possible
- Use consistent context handling pattern

## Implementation Instructions for LLM

For each file, make these changes:

1. **Standardize assertions**: Replace inconsistent error checking with this pattern:

   ```go
   if err != nil {
       t.Fatalf("unexpected error: %v", err)
   }
   ```

   Use `testboil.FailTestIfDiff(t, got, want)` for value comparisons when available.

2. **Add t.Helper()**: Add this to any function that calls `t.Error*` or `t.Fatal*` and is called by tests.

3. **Standardize context usage**: Replace manual context timeout patterns with consistent helper usage.

4. **Improve subtest naming**: Make subtest names descriptive of what they test, not how they test it.

**Priority**: Start with naming conventions (highest impact, lowest risk), then assertions, then context handling.
**IMPORTANT**: Ensure test parity: no testing functionality may be lost in these refactors.
