# Go 面试题 (第2组)

来源: 牛客网智能刷题 - Go专项

---

## Q1：下面属于关键字的是（）

**新手答**：见下方选项。

**高手答**：
-    func
-    def
-    struct
-    class

**差距在哪**：新手可能只凭直觉选，高手能准确理解 Go 语言的设计原理和最佳实践。

---

## Q2：有如下一段 Go 程序： package main import "fmt...

有如下一段 Go 程序：


package&nbsp;main

import&nbsp;"fmt"

func&nbsp;main()&nbsp;{
&nbsp;&nbsp;&nbsp;&nbsp;defer&nbsp;func()&nbsp;{
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;if&nbsp;err&nbsp;:=&nbsp;recover();&nbsp;err&nbsp;!=&nbsp;nil&nbsp;{
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;fmt.Println("发生异常：",&nbsp;err)
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;}
&nbsp;&nbsp;&nbsp;&nbsp;}()

&nbsp;&nbsp;&nbsp;&nbsp;a,&nbsp;b&nbsp;:=&nbsp;1,&nbsp;0
&nbsp;&nbsp;&nbsp;&nbsp;fmt.Println(a&nbsp;/&nbsp;b)
}
下面选项中说法正确的是（）

**新手答**：见下方选项。

**高手答**：
-    上面代码中，使用 defer 定义了一个匿名函数，该函数中使用 recover 函数可以捕获可能发生的异常
-    上面代码中，会发生异常，程序会输出异常信息并继续执行
-    上面代码中，会发生异常，程序会输出异常信息并退出
-    上面代码中，不会发生异常，程序会正常输出 a / b 的结果

**差距在哪**：新手可能只凭直觉选，高手能准确理解 Go 语言的设计原理和最佳实践。

---

## Q3：关于接口，下面说法正确的是（）

**新手答**：见下方选项。

**高手答**：
-    只要两个接口拥有相同的方法列表（次序不同不要紧），那么它们就是等价的，可以相互赋值
-    如果接口A的方法列表是接口B的方法列表的子集，那么接口B可以赋值给接口A
-    接口查询是否成功，要在运行期才能够确定
-    接口赋值是否可行，要在运行期才能够确定

**差距在哪**：新手可能只凭直觉选，高手能准确理解 Go 语言的设计原理和最佳实践。

---

## Q4： 关于const常量定义，下面正确的使用方式是（）

关于const常量定义，下面正确的使用方式是（）

**新手答**：见下方选项。

**高手答**：
-    A
-    B
-    C
-    D

**差距在哪**：新手可能只凭直觉选，高手能准确理解 Go 语言的设计原理和最佳实践。

---

## Q5：下面 Go 程序有错误的地方是哪几处（） package main imp...

下面 Go 程序有错误的地方是哪几处（）


package&nbsp;main

import&nbsp;(
	fmt&nbsp;&nbsp;&nbsp;&nbsp;//&nbsp;1
)

func&nbsp;hello(num&nbsp;...int)&nbsp;{
	num[0]&nbsp;=&nbsp;18&nbsp;&nbsp;&nbsp;&nbsp;//&nbsp;2
}

func&nbsp;main()&nbsp;{
	i&nbsp;=&nbsp;[]int{5,&nbsp;6,&nbsp;7}&nbsp;&nbsp;&nbsp;&nbsp;//&nbsp;3
	hello(i...)&nbsp;&nbsp;&nbsp;&nbsp;//&nbsp;4
	fmt.Println(i[0])
}

**新手答**：见下方选项。

**高手答**：
-    1
-    2
-    3
-    4

**差距在哪**：新手可能只凭直觉选，高手能准确理解 Go 语言的设计原理和最佳实践。

---

