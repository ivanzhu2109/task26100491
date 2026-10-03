# x12val

X12 流式分帧与信封校验库（Go）。仅支持 00501，不校验交易正文业务字段。

## 接口

```go
v := x12val.NewValidator(func(ev x12val.Event) { /* 同步接收事件 */ })
v.Feed(chunk) // 可任意分块，多次调用
v.Finish()    // 输入结束，报告截断与未关闭信封
```

- `Event` 通过 `Kind` 区分：`EventSegment`（`Segment` 非空）或
  `EventError`（`Error` 非空）。
- `SegmentEvent`：段标签 `Tag`、解析后的 `Elements`、段首 0 基绝对字节
  偏移 `Offset`（计入段间 CR/LF）。ISA 段只按元素分隔符切分（16 个定宽
  字段，`Repeats` 为 nil）；其他段按元素/重复/组件三级切分，各层中间及
  末尾空值原样保留，字段空格不裁剪。
- `ErrorEvent`：稳定代码 `Code`（如 `SE_COUNT_MISMATCH`、
  `IEA_CONTROL_MISMATCH`、`MISSING_SE`、`TRUNCATED_ISA` 等）、触发段的
  段首偏移 `Offset`（EOF 类错误使用输入总长度），计数/控制号不符时
  `Expected`/`Actual` 分别给出尾段声明值与实际值。

## 性质

- 同一字节流任意分块，事件序列完全相同。
- 计数或控制号错误不影响后续仍可分帧的交易。
- 缺尾恢复出的新信封不继承旧信封的计数或控制号。
- 工作内存为常数级（只缓冲当前残段），不保存输入、已输出段或历史诊断。

## 测试

```sh
go test ./...
```
