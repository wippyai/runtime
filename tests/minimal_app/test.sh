#!/bin/bash
set -e

RUNNER="../../dist/runner-linux-amd64"

echo "=== Testing minimal_app ==="
echo ""

echo "1. Testing 'update' command..."
UPDATE_OUTPUT=$($RUNNER update -l wippy.lock -vv 2>&1)

echo "$UPDATE_OUTPUT" | grep -q "Excluding directories from source scanning" && echo "   ✅ Exclusion logging works" || (echo "   ❌ No exclusion logging" && exit 1)
echo "$UPDATE_OUTPUT" | grep -q '".wippy"' && echo "   ✅ Excludes .wippy" || (echo "   ❌ .wippy not excluded" && exit 1)
echo "$UPDATE_OUTPUT" | grep -q '"replacements/test_module"' && echo "   ✅ Excludes replacements/test_module" || (echo "   ❌ replacements not excluded" && exit 1)
echo "$UPDATE_OUTPUT" | grep -q "Update completed successfully" && echo "   ✅ Update completed" || (echo "   ❌ Update failed" && exit 1)
echo ""

echo "2. Testing 'run' command..."
RUN_OUTPUT=$(timeout 2 $RUNNER run -l wippy.lock -vv 2>&1 || true)

echo "$RUN_OUTPUT" | grep -q "Excluding directories from source scanning" && echo "   ✅ Exclusion logging works" || (echo "   ❌ No exclusion logging" && exit 1)
echo "$RUN_OUTPUT" | grep -q '".wippy/vendor"' && echo "   ✅ Excludes .wippy/vendor" || (echo "   ❌ .wippy/vendor not excluded" && exit 1)
echo "$RUN_OUTPUT" | grep -q '"replacements/test_module"' && echo "   ✅ Excludes replacements/test_module" || (echo "   ❌ replacements not excluded" && exit 1)
echo ""

echo "3. Verifying exclusion..."
if echo "$RUN_OUTPUT" | grep -qi "should.not.load"; then
    echo "   ❌ Failed: replacements were NOT excluded"
    exit 1
else
    echo "   ✅ Replacements correctly excluded"
fi
echo ""

echo "=== All tests passed! ==="

