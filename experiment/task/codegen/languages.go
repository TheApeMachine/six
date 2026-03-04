package codegen

type LanguageTask struct {
	Language string
	Corpus   []string
	Prompts  []PromptMeta
}

type PromptMeta struct {
	Prefix, Desc string
}

func GetLanguageTasks() []LanguageTask {
	return []LanguageTask{
		{
			Language: "Python",
			Corpus:   append(pythonCorpus(), longCorpus()...),
			Prompts: []PromptMeta{
				{"def factorial(n):", "Factorial"},
				{"def find_max(lst):", "Find max"},
				{"def binary_search(lst, target):", "Binary search"},
				{"def quicksort(lst):", "Quicksort"},
			},
		},
		{
			Language: "Go",
			Corpus:   goCorpus(),
			Prompts: []PromptMeta{
				{"func factorial(n int) int {", "Factorial"},
				{"func findMax(lst []int) int {", "Find max"},
				{"func binarySearch(lst []int, target int) int {", "Binary search"},
				{"func quicksort(lst []int) []int {", "Quicksort"},
			},
		},
		{
			Language: "C++",
			Corpus:   cppCorpus(),
			Prompts: []PromptMeta{
				{"int factorial(int n) {", "Factorial"},
				{"int findMax(const vector<int>& lst) {", "Find max"},
				{"int binarySearch(const vector<int>& lst, int target) {", "Binary search"},
				{"vector<int> quicksort(vector<int> lst) {", "Quicksort"},
			},
		},
		{
			Language: "Haskell",
			Corpus:   haskellCorpus(),
			Prompts: []PromptMeta{
				{"factorial :: Integer -> Integer\nfactorial 0 =", "Factorial"},
				{"findMax :: Ord a => [a] -> a\nfindMax", "Find max"},
				{"binarySearch ::", "Binary search"},
				{"quicksort :: Ord a => [a] -> [a]\nquicksort [] =", "Quicksort"},
			},
		},
		{
			Language: "Lisp",
			Corpus:   lispCorpus(),
			Prompts: []PromptMeta{
				{"(defun factorial (n)", "Factorial"},
				{"(defun find-max (lst)", "Find max"},
				{"(defun binary-search (lst target)", "Binary search"},
				{"(defun quicksort (lst)", "Quicksort"},
			},
		},
	}
}

func goCorpus() []string {
	return []string{
		"func factorial(n int) int {\n\tif n <= 1 {\n\t\treturn 1\n\t}\n\treturn n * factorial(n-1)\n}",
		"func findMax(lst []int) int {\n\tbest := lst[0]\n\tfor _, x := range lst[1:] {\n\t\tif x > best {\n\t\t\tbest = x\n\t\t}\n\t}\n\treturn best\n}",
		"func binarySearch(lst []int, target int) int {\n\tlow, high := 0, len(lst)-1\n\twhile low <= high {\n\t\tmid := (low + high) / 2\n\t\tif lst[mid] == target {\n\t\t\treturn mid\n\t\t} else if lst[mid] < target {\n\t\t\tlow = mid + 1\n\t\t} else {\n\t\t\thigh = mid - 1\n\t\t}\n\t}\n\treturn -1\n}",
		"func quicksort(lst []int) []int {\n\tif len(lst) <= 1 {\n\t\treturn lst\n\t}\n\tpivot := lst[len(lst)/2]\n\tvar left, middle, right []int\n\tfor _, x := range lst {\n\t\tif x < pivot {\n\t\t\tleft = append(left, x)\n\t\t} else if x == pivot {\n\t\t\tmiddle = append(middle, x)\n\t\t} else {\n\t\t\tright = append(right, x)\n\t\t}\n\t}\n\treturn append(append(quicksort(left), middle...), quicksort(right)...)\n}",
	}
}

func cppCorpus() []string {
	return []string{
		"int factorial(int n) {\n\tif (n <= 1) return 1;\n\treturn n * factorial(n - 1);\n}",
		"int findMax(const vector<int>& lst) {\n\tint best = lst[0];\n\tfor(int i=1; i<lst.size(); ++i) {\n\t\tif(lst[i] > best) best = lst[i];\n\t}\n\treturn best;\n}",
		"int binarySearch(const vector<int>& lst, int target) {\n\tint low = 0, high = lst.size() - 1;\n\twhile (low <= high) {\n\t\tint mid = low + (high - low) / 2;\n\t\tif (lst[mid] == target) return mid;\n\t\telse if (lst[mid] < target) low = mid + 1;\n\t\telse high = mid - 1;\n\t}\n\treturn -1;\n}",
		"vector<int> quicksort(vector<int> lst) {\n\tif (lst.size() <= 1) return lst;\n\tint pivot = lst[lst.size() / 2];\n\tvector<int> left, middle, right;\n\tfor (int x : lst) {\n\t\tif (x < pivot) left.push_back(x);\n\t\telse if (x == pivot) middle.push_back(x);\n\t\telse right.push_back(x);\n\t}\n\tvector<int> res = quicksort(left);\n\tres.insert(res.end(), middle.begin(), middle.end());\n\tvector<int> r = quicksort(right);\n\tres.insert(res.end(), r.begin(), r.end());\n\treturn res;\n}",
	}
}

func haskellCorpus() []string {
	return []string{
		"factorial :: Integer -> Integer\nfactorial 0 = 1\nfactorial n = n * factorial (n - 1)",
		"findMax :: Ord a => [a] -> a\nfindMax [x] = x\nfindMax (x:xs) = max x (findMax xs)",
		"binarySearch :: Ord a => a -> [a] -> Bool\nbinarySearch _ [] = False\nbinarySearch x lst\n  | x == mid = True\n  | x < mid  = binarySearch x left\n  | otherwise = binarySearch x right\n  where\n    (left, mid:right) = splitAt (length lst `div` 2) lst",
		"quicksort :: Ord a => [a] -> [a]\nquicksort [] = []\nquicksort (p:xs) = (quicksort lesser) ++ [p] ++ (quicksort greater)\n  where\n    lesser  = filter (< p) xs\n    greater = filter (>= p) xs",
	}
}

func lispCorpus() []string {
	return []string{
		"(defun factorial (n)\n  (if (<= n 1)\n      1\n      (* n (factorial (- n 1)))))",
		"(defun find-max (lst)\n  (if (= (length lst) 1)\n      (car lst)\n      (max (car lst) (find-max (cdr lst)))))",
		"(defun binary-search (lst target)\n  (let* ((len (length lst))\n         (mid (truncate len 2))\n         (mid-v (nth mid lst)))\n    (cond ((null lst) nil)\n          ((= target mid-v) mid)\n          ((< target mid-v) (binary-search (subseq lst 0 mid) target))\n          (t (binary-search (subseq lst (+ mid 1)) target)))))",
		"(defun quicksort (lst)\n  (if (null lst)\n      nil\n      (let* ((p (car lst))\n             (rest (cdr lst)))\n        (append (quicksort (remove-if-not (lambda (x) (< x p)) rest))\n                (list p)\n                (quicksort (remove-if-not (lambda (x) (>= x p)) rest))))))",
	}
}
