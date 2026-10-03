# task26100491

`x12val`：X12 流式分帧与信封校验库（仅 00501，不校验交易正文业务字段）。

## 接口

```go
v := x12val.NewValidator(func(e x12val.Event) {
    switch {
    case e.Segment != nil: // 完整段：Tag / Elements / Offset（段首 0 基绝对偏移）
    case e.Error != nil:   // 错误：Code / Offset（触发段段首偏移；EOF 错误为输入总长度）/ Detail
    }
})
v.Feed(chunk) // 可任意分块；同一字节流的事件序列与分块方式无关
v.Finish()    // 报告 EOF 残段（TRUNCATED_SEGMENT）及未关闭信封
```

- `Segment.Elements`：ISA 仅按元素分隔符切分（ISA11/ISA16 视为普通值）；
  其他段按元素 / 重复 / 组件三级切分，保留各层空值，不裁剪空格。
- 事件顺序：段先隐式关闭内层信封时，缺尾错误（内层优先）先于该段事件；
  尾段自身的计数 / 控制号错误紧随其段事件之后。
- 错误码：`TRUNCATED_SEGMENT`、`MISSING_SE/GE/IEA`、
  `SE/GE/IEA_COUNT_MISMATCH`、`SE/GE/IEA_CONTROL_MISMATCH`。
- 计数按十进制比较，控制号按原字节比较；缺尾子信封仍计入祖先计数；
  新信封不继承旧信封的计数或控制号。
- 工作内存与已处理段数无关，只与当前残段大小有关。

## 测试

```sh
go test ./...
```
