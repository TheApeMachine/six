#!/usr/bin/env python3
"""Export a tiny JSON AST for github.com/theapemachine/six/pkg/lang/pysix.

Reads Python source from stdin, prints one JSON object on stdout. Exit non-zero
on parse errors.

Supported statement subset: Assign, AugAssign, Expr, If, For (range only),
While (numeric bound + compare test, unrolled), FunctionDef (only if body is
a single Return with an expression; preserved for round-trip but calls are not
compiled — use module-level code for v1).

Unsupported constructs produce an {"error": "..."} wrapper instead of raising.
"""

from __future__ import annotations

import ast
import json
import sys


class Unsupported(Exception):
    pass


def binop_name(op: ast.operator) -> str:
    return type(op).__name__


def unaryop_name(op: ast.unaryop) -> str:
    return type(op).__name__


def cmpop_name(op: ast.cmpop) -> str:
    return type(op).__name__


def export_expr(n: ast.expr) -> dict:
    if isinstance(n, ast.Constant):
        if isinstance(n.value, bool):
            return {"kind": "Num", "n": int(n.value)}
        if isinstance(n.value, int):
            return {"kind": "Num", "n": n.value}
        raise Unsupported("only int/bool constants")
    if isinstance(n, ast.Name):
        return {"kind": "Name", "id": n.id}
    if isinstance(n, ast.BinOp):
        return {
            "kind": "BinOp",
            "op": binop_name(n.op),
            "left": export_expr(n.left),
            "right": export_expr(n.right),
        }
    if isinstance(n, ast.UnaryOp):
        return {
            "kind": "UnaryOp",
            "op": unaryop_name(n.op),
            "operand": export_expr(n.operand),
        }
    if isinstance(n, ast.Compare):
        if len(n.ops) != 1 or len(n.comparators) != 1:
            raise Unsupported("chained comparisons not supported")
        return {
            "kind": "Compare",
            "op": cmpop_name(n.ops[0]),
            "left": export_expr(n.left),
            "right": export_expr(n.comparators[0]),
        }
    if isinstance(n, ast.Call):
        if isinstance(n.func, ast.Name) and n.func.id == "range" and len(n.args) == 1:
            return {
                "kind": "Range",
                "stop": export_expr(n.args[0]),
            }
        raise Unsupported("only range(n) calls in for-loops")
    raise Unsupported(f"expression {type(n).__name__}")


def export_stmt(n: ast.stmt) -> dict:
    if isinstance(n, ast.Assign):
        if len(n.targets) != 1:
            raise Unsupported("multiple assignment targets")
        return {
            "kind": "Assign",
            "target": export_expr(n.targets[0]),
            "value": export_expr(n.value),
        }
    if isinstance(n, ast.AnnAssign):
        raise Unsupported("annotated assign")
    if isinstance(n, ast.AugAssign):
        if not isinstance(n.target, ast.Name):
            raise Unsupported("aug assign to non-name")
        return {
            "kind": "AugAssign",
            "target_id": n.target.id,
            "op": binop_name(n.op),
            "value": export_expr(n.value),
        }
    if isinstance(n, ast.Expr):
        return {"kind": "ExprStmt", "value": export_expr(n.value)}
    if isinstance(n, ast.If):
        return {
            "kind": "If",
            "test": export_expr(n.test),
            "body": [export_stmt(x) for x in n.body],
            "orelse": [export_stmt(x) for x in n.orelse],
        }
    if isinstance(n, ast.While):
        return {
            "kind": "While",
            "test": export_expr(n.test),
            "body": [export_stmt(x) for x in n.body],
        }
    if isinstance(n, ast.For):
        if not isinstance(n.target, ast.Name):
            raise Unsupported("for loop target must be a simple name")
        if not isinstance(n.iter, ast.Call):
            raise Unsupported("for loop range only")
        if not isinstance(n.iter.func, ast.Name) or n.iter.func.id != "range":
            raise Unsupported("for loop must be range(...)")
        if len(n.iter.args) != 1:
            raise Unsupported("range(stop) only")
        if n.iter.keywords:
            raise Unsupported("range keywords")
        return {
            "kind": "ForRange",
            "var": n.target.id,
            "stop": export_expr(n.iter.args[0]),
            "body": [export_stmt(x) for x in n.body],
        }
    if isinstance(n, ast.FunctionDef):
        if n.args.vararg or n.args.kwarg or n.args.kwonlyargs or n.args.posonlyargs:
            raise Unsupported("only plain positional args")
        if n.decorator_list:
            raise Unsupported("decorators")
        if len(n.body) != 1 or not isinstance(n.body[0], ast.Return):
            raise Unsupported("function must be single return")
        ret = n.body[0]
        if ret.value is None:
            raise Unsupported("empty return")
        rarg = [a.arg for a in n.args.args]
        return {
            "kind": "FuncDef",
            "name": n.name,
            "args": rarg,
            "value": export_expr(ret.value),
        }
    if isinstance(n, ast.Pass):
        return {"kind": "Pass"}
    raise Unsupported(f"statement {type(n).__name__}")


def export_module(tree: ast.Module) -> dict:
    body = []
    for n in tree.body:
        try:
            body.append(export_stmt(n))
        except Unsupported as exc:
            return {"error": str(exc)}
    return {"kind": "Module", "body": body}


def main() -> None:
    src = sys.stdin.read()
    try:
        tree = ast.parse(src)
    except SyntaxError as exc:
        err = {"error": f"syntax: {exc}"}
        json.dump(err, sys.stdout)
        sys.stdout.write("\n")
        sys.exit(1)
    if not isinstance(tree, ast.Module):
        json.dump({"error": "expected module"}, sys.stdout)
        sys.stdout.write("\n")
        sys.exit(1)
    out = export_module(tree)
    json.dump(out, sys.stdout)
    sys.stdout.write("\n")
    if "error" in out:
        sys.exit(2)


if __name__ == "__main__":
    main()
