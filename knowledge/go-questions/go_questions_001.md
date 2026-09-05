# Go 面试题 (第1组)

来源: 牛客网智能刷题 - Go专项

---

## Q1：Golang不支持自动垃圾回收,这一说法是否正确。

**新手答**：见下方选项。

**高手答**：
-    true
-    false

**差距在哪**：新手可能只凭直觉选，高手能准确理解 Go 语言的设计原理和最佳实践。

---

## Q2：考虑以下 Go 代码片段。该程序执行后，最有可能的输出是什么？ packa...

考虑以下 Go 代码片段。该程序执行后，最有可能的输出是什么？     package main
import (
    &quot;fmt&quot;
    &quot;sync&quot;
)
func main() {
    values := []int{10, 20, 30}
    var wg sync.WaitGroup
    wg.Add(len(values))
    for _, val := range values {
        go func() {
            fmt.Printf(&quot;%d &quot;, val)
            wg.Done()
        }()
    }
    wg.Wait()
}

**新手答**：见下方选项。

**高手答**：
-    10 20 30 （顺序可能不同）
-    30 30 30 （顺序可能不同）
-    程序会发生 panic，因为存在数据竞争
-    10 10 10 （顺序可能不同）

**差距在哪**：新手可能只凭直觉选，高手能准确理解 Go 语言的设计原理和最佳实践。

---

## Q3：对于以下代码，正确的是？ package main import &quo...

对于以下代码，正确的是？       package main

import &quot;fmt&quot;

func main() {

	ch := make(chan struct{})
	defer close(ch)

	go func() {
		ch &lt;- struct{}{}
	}()

	i := 0
	for range ch {
		i++
	}

	fmt.Printf(&quot;%d&quot;, i)
}

**新手答**：见下方选项。

**高手答**：
-    发生死锁。
-    输出 2
-    输出 1
-    输出 0

**差距在哪**：新手可能只凭直觉选，高手能准确理解 Go 语言的设计原理和最佳实践。

---

## Q4：某服务处理大文件，常用 s := data[i:j] 取子切片后长期缓存。...

某服务处理大文件，常用&nbsp;s&nbsp;:=&nbsp;data[i:j]&nbsp;取子切片后长期缓存。线上观察到内存占用持续升高。以下哪种做法最能避免因底层数组被大切片引用而导致的内存滞留？

**新手答**：见下方选项。

**高手答**：
-    将 s 的长度设为 0（s = s[:0]），可以立刻让底层数组被回收
-    在生成子切片后立刻调用 runtime.GC()
-    拷贝所需数据到一个新的切片，例如 bs := append([]byte(nil), data[i:j]...)
-    用 cap 限制子切片容量，例如 s = s[:j-i:j-i]，可以减少底层数组持有

**差距在哪**：新手可能只凭直觉选，高手能准确理解 Go 语言的设计原理和最佳实践。

---

## Q5：在Go 1.13及以上版本中，关于errors包的Is和As函数，**正确...

在Go&nbsp;1.13及以上版本中，关于errors包的Is和As函数，**正确**的是？

**新手答**：见下方选项。

**高手答**：
-    errors.Is(err, target)可判断err的错误链中是否包含target（无论target是否被Wrap）
-    errors.As(err, &target)要求target必须是一个已初始化的非nil值
-    使用fmt.Errorf("err: %v", originalErr)可以创建包含originalErr的错误链
-    errors.Is只能检查直接返回的错误，无法处理嵌套的Wrap错误

**差距在哪**：新手可能只凭直觉选，高手能准确理解 Go 语言的设计原理和最佳实践。

---

