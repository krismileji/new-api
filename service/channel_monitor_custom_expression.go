package service

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/tidwall/gjson"
)

// Only an explicit '=' prefix opts into arithmetic; ordinary GJSON paths keep
// their existing meaning, including keys containing operators and query syntax.
func extractChannelMonitorCustomJSONValue(body []byte, valuePath string) (float64, error) {
	if !strings.HasPrefix(valuePath, "=") {
		return extractChannelMonitorCustomJSONNumber(body, valuePath)
	}
	node, err := parseChannelMonitorCustomExpression(valuePath)
	if err != nil {
		return 0, err
	}
	return evaluateChannelMonitorCustomExpression(node, body)
}

func extractChannelMonitorCustomJSONNumber(body []byte, path string) (float64, error) {
	value := gjson.GetBytes(body, path)
	if !value.Exists() || value.Type == gjson.Null {
		return 0, fmt.Errorf("结果路径 %q 不存在", path)
	}
	var rawValue string
	switch value.Type {
	case gjson.Number:
		rawValue = value.Raw
	case gjson.String:
		rawValue = value.String()
	default:
		return 0, fmt.Errorf("结果路径 %q 的值不是数字", path)
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(rawValue), 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("结果路径 %q 的值不是有效数字", path)
	}
	return number, nil
}

func parseChannelMonitorCustomExpression(valuePath string) (ast.Node, error) {
	if len(valuePath) > maxChannelMonitorCustomResultPath {
		return nil, errors.New("JSON 取值路径或表达式不能超过 512 个字符")
	}
	tree, err := parser.Parse(strings.TrimSpace(strings.TrimPrefix(valuePath, "=")))
	if err != nil {
		return nil, errors.New("计算表达式格式无效，请使用 json(\"路径\")、数字、括号和加减乘除")
	}
	if _, err := evaluateChannelMonitorCustomExpression(tree.Node, nil); err != nil {
		return nil, err
	}
	return tree.Node, nil
}

// A nil body validates the allowed syntax without resolving paths or doing
// arithmetic. Evaluate only this small AST subset, never arbitrary expressions.
func evaluateChannelMonitorCustomExpression(node ast.Node, body []byte) (float64, error) {
	var value float64
	switch node := node.(type) {
	case *ast.IntegerNode:
		value = float64(node.Value)
	case *ast.FloatNode:
		value = node.Value
	case *ast.UnaryNode:
		if node.Operator != "+" && node.Operator != "-" {
			return 0, errors.New("计算表达式仅支持正负号和加减乘除")
		}
		operand, err := evaluateChannelMonitorCustomExpression(node.Node, body)
		if err != nil {
			return 0, err
		}
		value = operand
		if node.Operator == "-" {
			value = -value
		}
	case *ast.BinaryNode:
		if node.Operator != "+" && node.Operator != "-" && node.Operator != "*" && node.Operator != "/" {
			return 0, errors.New("计算表达式仅支持加减乘除和括号")
		}
		left, err := evaluateChannelMonitorCustomExpression(node.Left, body)
		if err != nil {
			return 0, err
		}
		right, err := evaluateChannelMonitorCustomExpression(node.Right, body)
		if err != nil {
			return 0, err
		}
		if body == nil {
			return 0, nil
		}
		switch node.Operator {
		case "+":
			value = left + right
		case "-":
			value = left - right
		case "*":
			value = left * right
		case "/":
			if right == 0 {
				return 0, errors.New("计算表达式的除数不能为 0")
			}
			value = left / right
		}
	case *ast.CallNode:
		function, ok := node.Callee.(*ast.IdentifierNode)
		if !ok || function.Value != "json" || len(node.Arguments) != 1 {
			return 0, errors.New("计算表达式取值必须使用 json(\"路径\")")
		}
		path, ok := node.Arguments[0].(*ast.StringNode)
		if !ok || strings.TrimSpace(path.Value) == "" {
			return 0, errors.New("json() 必须包含一个非空的 JSON 路径字符串")
		}
		if body == nil {
			return 0, nil
		}
		return extractChannelMonitorCustomJSONNumber(body, path.Value)
	default:
		return 0, errors.New("计算表达式仅支持 json(\"路径\")、数字、括号和加减乘除")
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, errors.New("计算表达式产生了无效数字或数值溢出")
	}
	return value, nil
}
