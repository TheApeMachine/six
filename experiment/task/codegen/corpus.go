package codegen

import "strings"

// pythonCorpus provides a small, deterministic corpus of Python function bodies
// for the span solver to learn from. Each entry is a complete function.
func pythonCorpus() []string {
	return []string{
		// Arithmetic / math
		"def factorial(n):\n    if n <= 1:\n        return 1\n    return n * factorial(n - 1)",
		"def fibonacci(n):\n    if n <= 1:\n        return n\n    a, b = 0, 1\n    for i in range(2, n + 1):\n        a, b = b, a + b\n    return b",
		"def gcd(a, b):\n    while b:\n        a, b = b, a % b\n    return a",
		"def lcm(a, b):\n    return a * b // gcd(a, b)",
		"def power(base, exp):\n    if exp == 0:\n        return 1\n    return base * power(base, exp - 1)",
		"def is_prime(n):\n    if n < 2:\n        return False\n    for i in range(2, int(n ** 0.5) + 1):\n        if n % i == 0:\n            return False\n    return True",
		"def abs_val(x):\n    if x < 0:\n        return -x\n    return x",
		"def max_val(a, b):\n    if a > b:\n        return a\n    return b",
		"def min_val(a, b):\n    if a < b:\n        return a\n    return b",
		"def sum_list(lst):\n    total = 0\n    for x in lst:\n        total += x\n    return total",

		// List operations
		"def reverse_list(lst):\n    result = []\n    for i in range(len(lst) - 1, -1, -1):\n        result.append(lst[i])\n    return result",
		"def find_max(lst):\n    if not lst:\n        return None\n    best = lst[0]\n    for x in lst[1:]:\n        if x > best:\n            best = x\n    return best",
		"def find_min(lst):\n    if not lst:\n        return None\n    best = lst[0]\n    for x in lst[1:]:\n        if x < best:\n            best = x\n    return best",
		"def contains(lst, target):\n    for x in lst:\n        if x == target:\n            return True\n    return False",
		"def count_occurrences(lst, target):\n    count = 0\n    for x in lst:\n        if x == target:\n            count += 1\n    return count",
		"def flatten(lst):\n    result = []\n    for item in lst:\n        if isinstance(item, list):\n            result.extend(flatten(item))\n        else:\n            result.append(item)\n    return result",
		"def unique(lst):\n    seen = set()\n    result = []\n    for x in lst:\n        if x not in seen:\n            seen.add(x)\n            result.append(x)\n    return result",
		"def zip_lists(a, b):\n    result = []\n    for i in range(min(len(a), len(b))):\n        result.append((a[i], b[i]))\n    return result",

		// String operations
		"def reverse_string(s):\n    return s[::-1]",
		"def is_palindrome(s):\n    return s == s[::-1]",
		"def count_chars(s):\n    counts = {}\n    for c in s:\n        counts[c] = counts.get(c, 0) + 1\n    return counts",
		"def capitalize_words(s):\n    words = s.split()\n    result = []\n    for w in words:\n        result.append(w[0].upper() + w[1:])\n    return ' '.join(result)",
		"def remove_duplicates(s):\n    seen = set()\n    result = []\n    for c in s:\n        if c not in seen:\n            seen.add(c)\n            result.append(c)\n    return ''.join(result)",

		// Sorting
		"def bubble_sort(lst):\n    n = len(lst)\n    for i in range(n):\n        for j in range(0, n - i - 1):\n            if lst[j] > lst[j + 1]:\n                lst[j], lst[j + 1] = lst[j + 1], lst[j]\n    return lst",
		"def insertion_sort(lst):\n    for i in range(1, len(lst)):\n        key = lst[i]\n        j = i - 1\n        while j >= 0 and lst[j] > key:\n            lst[j + 1] = lst[j]\n            j -= 1\n        lst[j + 1] = key\n    return lst",
		"def selection_sort(lst):\n    for i in range(len(lst)):\n        min_idx = i\n        for j in range(i + 1, len(lst)):\n            if lst[j] < lst[min_idx]:\n                min_idx = j\n        lst[i], lst[min_idx] = lst[min_idx], lst[i]\n    return lst",

		// Data structures
		"def binary_search(lst, target):\n    low, high = 0, len(lst) - 1\n    while low <= high:\n        mid = (low + high) // 2\n        if lst[mid] == target:\n            return mid\n        elif lst[mid] < target:\n            low = mid + 1\n        else:\n            high = mid - 1\n    return -1",
		"def merge_sorted(a, b):\n    result = []\n    i, j = 0, 0\n    while i < len(a) and j < len(b):\n        if a[i] <= b[j]:\n            result.append(a[i])\n            i += 1\n        else:\n            result.append(b[j])\n            j += 1\n    result.extend(a[i:])\n    result.extend(b[j:])\n    return result",

		// Higher-order / functional
		"def map_list(fn, lst):\n    result = []\n    for x in lst:\n        result.append(fn(x))\n    return result",
		"def filter_list(fn, lst):\n    result = []\n    for x in lst:\n        if fn(x):\n            result.append(x)\n    return result",
		"def reduce_list(fn, lst, initial):\n    acc = initial\n    for x in lst:\n        acc = fn(acc, x)\n    return acc",
	}
}

// longCorpus provides additional longer Python functions (80–150 tokens each)
// for testing long-range span chaining stability.
func longCorpus() []string {
	return []string{
		"def quicksort(lst):\n    if len(lst) <= 1:\n        return lst\n    pivot = lst[len(lst) // 2]\n    left = [x for x in lst if x < pivot]\n    middle = [x for x in lst if x == pivot]\n    right = [x for x in lst if x > pivot]\n    return quicksort(left) + middle + quicksort(right)",
		"def transpose(matrix):\n    if not matrix:\n        return []\n    rows = len(matrix)\n    cols = len(matrix[0])\n    result = []\n    for j in range(cols):\n        row = []\n        for i in range(rows):\n            row.append(matrix[i][j])\n        result.append(row)\n    return result",
		"def two_sum(nums, target):\n    seen = {}\n    for i in range(len(nums)):\n        complement = target - nums[i]\n        if complement in seen:\n            return [seen[complement], i]\n        seen[nums[i]] = i\n    return []",
		"def rle_encode(s):\n    if not s:\n        return ''\n    result = []\n    count = 1\n    for i in range(1, len(s)):\n        if s[i] == s[i - 1]:\n            count += 1\n        else:\n            result.append(s[i - 1] + str(count))\n            count = 1\n    result.append(s[-1] + str(count))\n    return ''.join(result)",
		"def dfs(graph, start):\n    visited = set()\n    stack = [start]\n    result = []\n    while stack:\n        node = stack.pop()\n        if node not in visited:\n            visited.add(node)\n            result.append(node)\n            for neighbor in graph.get(node, []):\n                if neighbor not in visited:\n                    stack.append(neighbor)\n    return result",
		"def bfs(graph, start):\n    visited = set()\n    queue = [start]\n    visited.add(start)\n    result = []\n    while queue:\n        node = queue.pop(0)\n        result.append(node)\n        for neighbor in graph.get(node, []):\n            if neighbor not in visited:\n                visited.add(neighbor)\n                queue.append(neighbor)\n    return result",
		"def merge_sort(lst):\n    if len(lst) <= 1:\n        return lst\n    mid = len(lst) // 2\n    left = merge_sort(lst[:mid])\n    right = merge_sort(lst[mid:])\n    result = []\n    i = 0\n    j = 0\n    while i < len(left) and j < len(right):\n        if left[i] <= right[j]:\n            result.append(left[i])\n            i += 1\n        else:\n            result.append(right[j])\n            j += 1\n    result.extend(left[i:])\n    result.extend(right[j:])\n    return result",
		"def group_by(lst, key_fn):\n    groups = {}\n    for item in lst:\n        key = key_fn(item)\n        if key not in groups:\n            groups[key] = []\n        groups[key].append(item)\n    return groups",
	}
}

// tokenize splits text into simple whitespace+punctuation tokens.
func tokenize(text string) []string {
	words := strings.Fields(text)
	var tokens []string
	for _, w := range words {
		tokens = append(tokens, w)
	}
	return tokens
}

// detokenize joins tokens back into text.
func detokenize(tokens []string) string {
	return strings.Join(tokens, " ")
}
