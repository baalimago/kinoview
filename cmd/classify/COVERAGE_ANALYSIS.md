# Classify Command - Coverage Analysis & Testing Plan

## Current Coverage: 35.4%

### Coverage Breakdown

#### ✅ COVERED (35.4%)
1. **Describe()** - Line 59
2. **Help()** - Line 63
3. **Flagset()** - Lines 137-145
   - Flag creation and initialization
   - Default values
   - Flag storage
4. **Command() factory** - Lines 40-52
   - Successful path (lines 40-52)
5. **Setup() - Partial** - Lines 67-87
   - Store creation (line 67)
   - Successful setup (line 87)

---

## ❌ UNCOVERED Branches (64.6%)

### 1. **Command() Error Paths** (Lines 40-50)
**Lines: 40-50** - Coverage: 0%

```go
configDir, err := os.UserConfigDir()
if err != nil {
    ancli.Errf("failed to find user config dir: %v", err)  // Line 42 - NOT COVERED
}
// ...
r, _ := os.Executable()
if err != nil {
    ancli.Errf("failed to create indexer: %v", err)        // Line 47-50 - NOT COVERED
    return nil
}
```

**Why uncovered:**
- `os.UserConfigDir()` rarely fails in test environments
- The error variable `err` is immediately ignored with `_` on line 47, so the condition on line 48 is dead code

**Design Issue:** The code has a bug - it ignores the error from `os.Executable()` but then checks an undefined `err` variable.

**Testing Strategy:**
- Use `t.Setenv()` to mock environment (though UserConfigDir reads actual system)
- Cannot easily test without mocking os package (would require dependency injection)
- **DIFFICULT**: Requires refactoring to accept configDir as parameter or use interface

---

### 2. **Setup() - Store Setup Error** (Lines 84-86)
**Lines: 84-86** - Coverage: 0%

```go
_, err := c.store.Setup(ctx)
if err != nil {
    return fmt.Errorf("failed to setup store: %w", err)  // NOT COVERED
}
```

**Why uncovered:**
- Mock storage always returns `nil` error in tests

**Testing Strategy:**
- ✅ **EASY**: Modify mockStorage to return error conditionally
- Add test: `TestCommand_Setup_store_error`
- Test that Setup() returns wrapped error when store.Setup() fails

---

### 3. **Run() - Interactive User Input** (Lines 90-133)
**Lines: 90-133** - Coverage: 0%

This is the MAIN function and it's completely untested!

```go
func (c *command) Run(ctx context.Context) error {
    errChan := make(chan error)
    go func() {
        err := c.store.StartClassificationStation(ctx)  // Line 92-96 - NOT COVERED
        if err != nil {
            errChan <- err
        }
    }()
    items := c.store.Snapshot()
    
    time.Sleep(time.Second)
    reader := bufio.NewReader(os.Stdin)
    ancli.Okf("Found: '%v' items. Filter by name (empty for all): ", len(items))
    filter, _ := reader.ReadString('\n')  // Line 106 - Interactive input - NOT COVERED
    filter = strings.TrimSpace(filter)
    
    // Filter items...
    
    ancli.Okf("Found: '%v' items. Proceed to classify? (y/N): ", len(filteredItems))
    resp, _ := reader.ReadString('\n')    // Line 113 - Interactive input - NOT COVERED
    resp = strings.TrimSpace(strings.ToLower(resp))
    
    if resp != "y" && resp != "yes" {
        return errors.New("user abort")     // Line 125 - User abort - NOT COVERED
    }
    
    for _, i := range filteredItems {
        c.store.AddToClassificationQueue(i)  // Line 127 - NOT COVERED
    }
    
    select {
    case <-ctx.Done():
        return nil                          // Line 129 - Context cancel - NOT COVERED
    case classifyErr := <-errChan:
        return classifyErr                  // Line 132 - Error from goroutine - NOT COVERED
    }
}
```

**Why uncovered:**
- Requires mocking stdin (bufio.Reader)
- Requires goroutine synchronization
- Interactive nature makes testing complex

**Design Issues:**
- Heavy coupling to `os.Stdin` makes testing difficult
- No way to inject Reader for testing
- Time.Sleep() makes tests slow
- Error channel handling is complex

**Testing Strategy:**

#### Option A: Refactor for Testability (RECOMMENDED)
Extract reader injection:
```go
func (c *command) Run(ctx context.Context) error {
    return c.RunWithReader(ctx, os.Stdin)
}

func (c *command) RunWithReader(ctx context.Context, input io.Reader) error {
    // ... use input instead of os.Stdin
}
```

#### Option B: Mock stdin (Workaround)
Use `io.Pipe()` to create mock stdin:
```go
r, w := io.Pipe()
w.Write([]byte("filter\ny\n"))  // Simulate user input
defer r.Close()
defer w.Close()
```

---

## Testing Plan - Prioritized

### Priority 1: HIGH IMPACT (Easy to implement)

#### Test 1.1: Setup Error Handling
```go
func TestCommand_Setup_store_error(t *testing.T) {
    // Test that Setup() returns error when store.Setup() fails
    // Modify mockStorage to return error
}
```
**Effort:** 30 minutes | **Coverage gain:** +2%

---

### Priority 2: MEDIUM IMPACT (Requires refactoring)

#### Test 2.1: Run() Basic Flow - Requires Reader Injection
```go
func TestCommand_Run_with_filter_and_approve(t *testing.T) {
    // Inject mock reader with "test\ny\n"
    // Verify items are queued
}
```

**Effort:** 1-2 hours | **Coverage gain:** +15%
**Prerequisite:** Refactor to accept `io.Reader` parameter

#### Test 2.2: Run() User Abort
```go
func TestCommand_Run_user_abort(t *testing.T) {
    // Inject mock reader with "test\nn\n" (no)
    // Verify error.New("user abort") is returned
}
```

**Effort:** 30 minutes (after 2.1) | **Coverage gain:** +3%

#### Test 2.3: Run() Classification Error
```go
func TestCommand_Run_classification_error(t *testing.T) {
    // Mock store to return error from StartClassificationStation
    // Verify error is returned
}
```

**Effort:** 30 minutes | **Coverage gain:** +2%

#### Test 2.4: Run() Context Cancellation
```go
func TestCommand_Run_context_cancel(t *testing.T) {
    // Create context that cancels during run
    // Verify nil error returned
}
```

**Effort:** 30 minutes | **Coverage gain:** +2%

---

### Priority 3: LOW IMPACT (Difficult, minimal benefit)

#### Test 3.1: Command() os.UserConfigDir Error
**Status:** SKIP - Dead code bug, requires major refactoring
**Reason:** 
- `os.UserConfigDir()` error is not actually checked (line 48 references wrong `err`)
- Would require dependency injection of `os.UserConfigDir`
- Minimal real-world impact
- **Fix recommendation:** Remove dead code or fix the bug

---

## Recommended Refactoring for Better Testability

### Current Issues:
1. **Hard-coded stdin** - `os.Stdin` cannot be mocked
2. **Hard-coded os.Executable()** - Cannot mock
3. **Hard-coded os.UserConfigDir()** - Cannot mock
4. **Time.Sleep()** - Makes tests slow
5. **Dead code bug** - Line 47-50 checks wrong error

### Refactored Design:

```go
type command struct {
    // ... existing fields ...
    
    // For testing
    reader io.Reader
    executablePath string
}

// Constructor with defaults
func Command() *command {
    return CommandWithDefaults(os.Stdin, "")
}

// Testable constructor
func CommandWithDefaults(reader io.Reader, execPath string) *command {
    if execPath == "" {
        execPath, _ = os.Executable()
    }
    // ...
}

// Testable Run method
func (c *command) Run(ctx context.Context) error {
    return c.runWithReader(ctx, c.reader)
}

func (c *command) runWithReader(ctx context.Context, reader io.Reader) error {
    // ... implementation using reader
}
```

---

## Summary Table

| Branch | Lines | Coverage | Effort | Impact | Notes |
|--------|-------|----------|--------|--------|-------|
| Command() success | 40-52 | ✅ | - | - | Already covered |
| Command() UserConfigDir error | 42 | ❌ | HARD | LOW | Dead code, skip |
| Command() Executable error | 47-50 | ❌ | HARD | LOW | Dead code, skip |
| Describe() | 59 | ✅ | - | - | Already covered |
| Help() | 63 | ✅ | - | - | Already covered |
| Setup() success | 67-87 | ✅ | - | - | Already covered |
| Setup() store error | 84-86 | ❌ | EASY | MED | +2% |
| Run() classification error | 92-96 | ❌ | MED | MED | +2% (needs refactor) |
| Run() filter input | 106 | ❌ | MED | HIGH | +5% (needs refactor) |
| Run() approval input | 113 | ❌ | MED | HIGH | +5% (needs refactor) |
| Run() user abort | 125 | ❌ | MED | MED | +3% (needs refactor) |
| Run() queue items | 127 | ❌ | MED | HIGH | +5% (needs refactor) |
| Run() context cancel | 129 | ❌ | MED | LOW | +2% (needs refactor) |
| Run() error return | 132 | ❌ | MED | LOW | +2% (needs refactor) |
| Flagset() | 137-145 | ✅ | - | - | Already covered |

---

## Quick Wins (No Refactoring Needed)

1. **Setup() store error** - 30 min, +2%
   - Just modify mockStorage to return error conditionally

---

## Realistic Target

**Without refactoring:** 37-40% coverage (quick wins only)
**With refactoring:** 80-85% coverage (full Run() testing)

**Recommended approach:** 
1. Implement quick wins first (+2%)
2. Plan refactoring for better testability
3. Add Run() tests after refactoring
