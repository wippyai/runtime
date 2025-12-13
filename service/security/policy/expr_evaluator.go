package policy

import (
	"fmt"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	policyapi "github.com/wippyai/runtime/api/service/security/policy"
)

// ExprEvaluator compiles and evaluates expr-lang expressions for policy evaluation
type ExprEvaluator struct {
	expression string
	program    *vm.Program
}

// NewExprEvaluator creates a new evaluator with a pre-compiled expression
func NewExprEvaluator(expression string) (*ExprEvaluator, error) {
	if expression == "" {
		return nil, policyapi.ErrExpressionEmpty
	}

	// Compile expression with AsBool to ensure boolean result
	// Don't specify Env type - let it be dynamic to support map access
	program, err := expr.Compile(expression,
		expr.AsBool(),                  // Expression must evaluate to boolean
		expr.AllowUndefinedVariables(), // Allow dynamic field access
	)
	if err != nil {
		return nil, policyapi.NewExprCompilationError(err)
	}

	return &ExprEvaluator{
		expression: expression,
		program:    program,
	}, nil
}

// Evaluate runs the compiled expression against the provided environment
// Returns true if the expression evaluates to true, false otherwise
func (e *ExprEvaluator) Evaluate(env map[string]any) (bool, error) {
	// Run the compiled program
	output, err := vm.Run(e.program, env)
	if err != nil {
		return false, policyapi.NewExprEvaluationError(err)
	}

	// Type assert to bool
	result, ok := output.(bool)
	if !ok {
		return false, policyapi.NewExprNotBooleanError(fmt.Sprintf("%T", output))
	}

	return result, nil
}

// Expression returns the original expression string
func (e *ExprEvaluator) Expression() string {
	return e.expression
}
