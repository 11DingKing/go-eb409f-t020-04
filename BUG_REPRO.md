# Bug Reproduction

## 包的性质

当前 test_model_fix 保存的是被测模型修复后的结果源码，不是初始含 Bug 源码。要复现原始缺陷，必须检出下面固定的 parent SHA；不要在当前修复结果源码上期待重新出现修复前失败。生成系统使用的可信验证补丁和完整验证日志仅在本地留存，不提交到结果分支。

## 问题现象

开了 -race 之后，一边下工单一边刷通知列表就报 data race，请帮我修复。

现象：值班台会持续轮询 GET /api/notifications，同时调度侧在不断产生新通知（工单升级、队列推进、紧急采购等）。两边并发时：

  - -race 版本直接打出 WARNING: DATA RACE，测试里紧接着是 race detected during execution of test 然后失败退出。
  - 关掉 -race 时偶尔会读到一条“空的”通知（遍历到 nil 元素），面板上就报错。
  - 对照：先把工单都处理完、再单独刷通知列表，一切正常。

期望：并发读取通知列表不能出现任何 data race，返回的每一条通知都必须是完整可用的，并且一条通知都不能丢。修复后请保证 go test -race -timeout=300s -count=1 ./... 全绿（-race 需要 CGO_ENABLED=1），不要修改或跳过测试。

## 含 Bug 版本

- 仓库：11DingKing/go-eb409f-t020-04
- 仓库地址：https://github.com/11DingKing/go-eb409f-t020-04.git
- parent SHA：c7781a70a8d60cb56c1ef2cb2ed522c59b4d45dd

## 复现步骤

```bash
git clone -- https://github.com/11DingKing/go-eb409f-t020-04.git bug-repro
cd bug-repro
git checkout --detach c7781a70a8d60cb56c1ef2cb2ed522c59b4d45dd
go test -race ./internal/dispatch -run "^TestNotificationsReadableWhileOrdersEscalate$" -count=1 -v
```

## 双架构完整错误信息

### linux/amd64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test -race ./internal/dispatch -run "^TestNotificationsReadableWhileOrdersEscalate$" -count=1 -v
=== RUN   TestNotificationsReadableWhileOrdersEscalate
==================
WARNING: DATA RACE
Write at 0x00c000192100 by goroutine 10:
  microgrid-ops/internal/dispatch.(*Orchestrator).addNotification()
      /app/internal/dispatch/dispatch.go:100 +0x42e
  microgrid-ops/internal/dispatch.(*Orchestrator).EscalateOrder()
      /app/internal/dispatch/dispatch.go:637 +0x924
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func1()
      /app/internal/dispatch/notifications_concurrent_test.go:40 +0x264
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.gowrap1()
      /app/internal/dispatch/notifications_concurrent_test.go:45 +0x38

Previous read at 0x00c000192100 by goroutine 13:
  microgrid-ops/internal/dispatch.(*Orchestrator).Notifications()
      /app/internal/dispatch/dispatch.go:85 +0xef
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func2()
      /app/internal/dispatch/notifications_concurrent_test.go:58 +0x171

Goroutine 10 (running) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:28 +0x5e4
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x38

Goroutine 13 (running) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:51 +0x92c
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c000102078 by goroutine 13:
  runtime.slicecopy()
      /usr/local/go/src/runtime/slice.go:392 +0x0
  microgrid-ops/internal/dispatch.(*Orchestrator).Notifications()
      /app/internal/dispatch/dispatch.go:86 +0x16e
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func2()
      /app/internal/dispatch/notifications_concurrent_test.go:58 +0x171

Previous write at 0x00c000102078 by goroutine 10:
  microgrid-ops/internal/dispatch.(*Orchestrator).addNotification()
      /app/internal/dispatch/dispatch.go:100 +0x3e7
  microgrid-ops/internal/dispatch.(*Orchestrator).EscalateOrder()
      /app/internal/dispatch/dispatch.go:637 +0x924
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func1()
      /app/internal/dispatch/notifications_concurrent_test.go:40 +0x264
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.gowrap1()
      /app/internal/dispatch/notifications_concurrent_test.go:45 +0x38

Goroutine 13 (running) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:51 +0x92c
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x38

Goroutine 10 (running) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:28 +0x5e4
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c000166100 by goroutine 13:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func2()
      /app/internal/dispatch/notifications_concurrent_test.go:63 +0x17b

Previous write at 0x00c000166100 by goroutine 10:
  microgrid-ops/internal/dispatch.(*Orchestrator).addNotification()
      /app/internal/dispatch/dispatch.go:92 +0x173
  microgrid-ops/internal/dispatch.(*Orchestrator).EscalateOrder()
      /app/internal/dispatch/dispatch.go:637 +0x924
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func1()
      /app/internal/dispatch/notifications_concurrent_test.go:40 +0x264
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.gowrap1()
      /app/internal/dispatch/notifications_concurrent_test.go:45 +0x38

Goroutine 13 (running) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:51 +0x92c
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x38

Goroutine 10 (running) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:28 +0x5e4
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Write at 0x00c000192100 by goroutine 10:
  microgrid-ops/internal/dispatch.(*Orchestrator).addNotification()
      /app/internal/dispatch/dispatch.go:100 +0x42e
  microgrid-ops/internal/dispatch.(*Orchestrator).EscalateOrder()
      /app/internal/dispatch/dispatch.go:637 +0x924
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func1()
      /app/internal/dispatch/notifications_concurrent_test.go:40 +0x264
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.gowrap1()
      /app/internal/dispatch/notifications_concurrent_test.go:45 +0x38

Previous read at 0x00c000192100 by goroutine 13:
  microgrid-ops/internal/dispatch.(*Orchestrator).Notifications()
      /app/internal/dispatch/dispatch.go:86 +0x145
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func2()
      /app/internal/dispatch/notifications_concurrent_test.go:58 +0x171

Goroutine 10 (running) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:28 +0x5e4
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x38

Goroutine 13 (running) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:51 +0x92c
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x38
==================
    testing.go:1712: race detected during execution of test
--- FAIL: TestNotificationsReadableWhileOrdersEscalate (0.13s)
FAIL
FAIL	microgrid-ops/internal/dispatch	0.190s
FAIL

```

stderr：

```text
(empty)
```

### linux/arm64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test -race ./internal/dispatch -run "^TestNotificationsReadableWhileOrdersEscalate$" -count=1 -v
=== RUN   TestNotificationsReadableWhileOrdersEscalate
==================
WARNING: DATA RACE
Read at 0x00c000016340 by goroutine 13:
  microgrid-ops/internal/dispatch.(*Orchestrator).Notifications()
      /app/internal/dispatch/dispatch.go:85 +0xb0
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func2()
      /app/internal/dispatch/notifications_concurrent_test.go:58 +0x114

Previous write at 0x00c000016340 by goroutine 10:
  microgrid-ops/internal/dispatch.(*Orchestrator).addNotification()
      /app/internal/dispatch/dispatch.go:100 +0x340
  microgrid-ops/internal/dispatch.(*Orchestrator).EscalateOrder()
      /app/internal/dispatch/dispatch.go:637 +0x63c
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func1()
      /app/internal/dispatch/notifications_concurrent_test.go:40 +0x198
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.gowrap1()
      /app/internal/dispatch/notifications_concurrent_test.go:45 +0x38

Goroutine 13 (running) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:51 +0x798
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x164
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x34

Goroutine 10 (running) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:28 +0x504
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x164
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x34
==================
==================
WARNING: DATA RACE
Read at 0x00c000016340 by goroutine 13:
  microgrid-ops/internal/dispatch.(*Orchestrator).Notifications()
      /app/internal/dispatch/dispatch.go:86 +0xf4
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func2()
      /app/internal/dispatch/notifications_concurrent_test.go:58 +0x114

Previous write at 0x00c000016340 by goroutine 10:
  microgrid-ops/internal/dispatch.(*Orchestrator).addNotification()
      /app/internal/dispatch/dispatch.go:100 +0x340
  microgrid-ops/internal/dispatch.(*Orchestrator).EscalateOrder()
      /app/internal/dispatch/dispatch.go:637 +0x63c
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func1()
      /app/internal/dispatch/notifications_concurrent_test.go:40 +0x198
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.gowrap1()
      /app/internal/dispatch/notifications_concurrent_test.go:45 +0x38

Goroutine 13 (running) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:51 +0x798
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x164
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x34

Goroutine 10 (running) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:28 +0x504
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x164
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x34
==================
==================
WARNING: DATA RACE
Read at 0x00c0000a8d80 by goroutine 13:
  runtime.slicecopy()
      /usr/local/go/src/runtime/slice.go:392 +0x0
  microgrid-ops/internal/dispatch.(*Orchestrator).Notifications()
      /app/internal/dispatch/dispatch.go:86 +0x110
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func2()
      /app/internal/dispatch/notifications_concurrent_test.go:58 +0x114

Previous write at 0x00c0000a8d80 by goroutine 11:
  microgrid-ops/internal/dispatch.(*Orchestrator).addNotification()
      /app/internal/dispatch/dispatch.go:100 +0x300
  microgrid-ops/internal/dispatch.(*Orchestrator).EscalateOrder()
      /app/internal/dispatch/dispatch.go:637 +0x63c
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func1()
      /app/internal/dispatch/notifications_concurrent_test.go:40 +0x198
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.gowrap1()
      /app/internal/dispatch/notifications_concurrent_test.go:45 +0x38

Goroutine 13 (running) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:51 +0x798
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x164
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x34

Goroutine 11 (finished) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:28 +0x504
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x164
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x34
==================
==================
WARNING: DATA RACE
Read at 0x00c0001a3980 by goroutine 13:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func2()
      /app/internal/dispatch/notifications_concurrent_test.go:63 +0x120

Previous write at 0x00c0001a3980 by goroutine 11:
  microgrid-ops/internal/dispatch.(*Orchestrator).addNotification()
      /app/internal/dispatch/dispatch.go:92 +0xfc
  microgrid-ops/internal/dispatch.(*Orchestrator).EscalateOrder()
      /app/internal/dispatch/dispatch.go:637 +0x63c
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.func1()
      /app/internal/dispatch/notifications_concurrent_test.go:40 +0x198
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate.gowrap1()
      /app/internal/dispatch/notifications_concurrent_test.go:45 +0x38

Goroutine 13 (running) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:51 +0x798
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x164
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x34

Goroutine 11 (finished) created at:
  microgrid-ops/internal/dispatch.TestNotificationsReadableWhileOrdersEscalate()
      /app/internal/dispatch/notifications_concurrent_test.go:28 +0x504
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:2036 +0x164
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:2101 +0x34
==================
    testing.go:1712: race detected during execution of test
--- FAIL: TestNotificationsReadableWhileOrdersEscalate (0.02s)
FAIL
FAIL	microgrid-ops/internal/dispatch	0.030s
FAIL

```

stderr：

```text
(empty)
```

## 通过条件

定向测试通过：go test -race ./internal/dispatch -run '^TestNotificationsReadableWhileOrdersEscalate$' -count=1 -v（CGO_ENABLED=1）
全量回归 go test -race -timeout=300s -count=1 ./... 通过，go build ./...、go vet ./... 与 gofmt -l . 干净
4 个并发写入方各产生 60 条通知期间持续读取通知列表：race detector 无告警、读到的每条通知均非 nil、结束后通知总数恰为 240；既有队列、升级与备件测试保持通过
