package policy

import (
	"fmt"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// ExprEvaluator compiles and evaluates expr-lang expressions for policy evaluation
type ExprEvaluator struct {
	expression string
	program    *vm.Program
}

// NewExprEvaluator creates a new evaluator with a pre-compiled expression
func NewExprEvaluator(expression string) (*ExprEvaluator, error) {
	if expression == "" {
		return nil, fmt.Errorf("expression cannot be empty")
	}

	// Compile expression with AsBool to ensure boolean result
	// Don't specify Env type - let it be dynamic to support map access
	program, err := expr.Compile(expression,
		expr.AsBool(),                  // Expression must evaluate to boolean
		expr.AllowUndefinedVariables(), // Allow dynamic field access
	)
	if err != nil {
		return nil, fmt.Errorf("expression compilation failed: %w", err)
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
		return false, fmt.Errorf("expression evaluation failed: %w", err)
	}

	// Type assert to bool
	result, ok := output.(bool)
	if !ok {
		return false, fmt.Errorf("expression did not return boolean, got %T", output)
	}

	return result, nil
}

// Expression returns the original expression string
func (e *ExprEvaluator) Expression() string {
	return e.expression
}
