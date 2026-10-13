# 与阿里云 OSS 的差异

iXoSS 实现了 OSS 的 `x-oss-process` 语法，但它不是 OSS。本文件是两者差异的
完整清单，以便迁移到 iXoSS 的服务可以在这里发现问题，而不是在生产环境中。

以下所有内容都是针对运行中的服务器实测的，不是从源代码中读取的。
当某行标注"OSS"时，指的是 OSS 文档的说明——链接在底部——
而不是与该服务的实时对比。

**迁移前请先阅读：** 最可能改变你的调用者已经在解析的响应的行是
[`info` 分辨率字段](#info) 和 [BMP 透明度](#格式转换)。
像素预算与 OSS 匹配；[单维度限制](#限制和默认值) 不匹配。

---

OSS 自己列表中的每个操作都已实现：`resize`、`crop`、`indexcrop`、
`trim`、`rotate`、`auto-orient`、`blur`、`sharpen`、`pixelate`、`bright`、
`contrast`、`circle`、`rounded-corners`、`watermark`、`quality`、`format`、
`interlace`、`info`、`average-hue`。保存的 `style/<name>` 预设通过
`IXOSS_STYLES` 配置，而不是 OSS 控制台。

## iXoSS 新增功能

| 功能 | 说明 |
|---|---|
| `format,auto` | 选择客户端 `Accept` 头允许且此构建可以编码的最佳格式，然后发送 `Vary: Accept`。最高 `q` 值胜出；`q=0` 和格式错误的 `q` 视为"不可接受"；`*/*` 永远不会被解读为"AVIF 可以"。 |
| `format,jxl` | JPEG XL 不在 OSS 的格式表中。参见 README 中的 JPEG XL 说明。 |
| `format,jpeg` | 作为 `jpg` 的别名接受。OSS 只拼写为 `jpg`。 |
| `format,f_png` | `key_value` 拼写与 OSS 的裸 `format,png` 一起被接受。 |
| `x-ixoss-signature` | 可选的 HMAC 请求签名，除非同时设置 `IXOSS_KEY` 和 `IXOSS_SALT`，否则关闭。参见 README。 |
| `GET /healthz` | 存活探针；免签名。 |

## 格式转换

| 情况 | OSS | iXoSS |
|---|---|---|
| 对非 GIF 源执行 `format,gif` | 保持源格式 | 相同——已验证 `jpg→jpeg`、`png→png`、`gif→gif` |
| WebP 输出宽度或高度超过 16383 px | 转换失败 | 按比例缩放并返回 `200`，日志中带有 `WARN`。16400×8 的源输出为 16383×8——0.1% 的重采样，不裁剪任何内容（通过标记两个边缘验证）。 |
| 透明度 → **BMP** | 填充白色 | 写入 32 位 BITMAPV5 保留 alpha。大多数解码器——包括 libvips 回读时——忽略 BMP alpha 并将透明区域渲染为**黑色**。 |
| 透明度 → JPEG | 填充白色 | 相同 |
| 透明度 → PNG / WebP | — | 保留 |
| 带 alpha 的 HEIC → `jpg` | 拒绝 | 扁平化为白色并转换 |
| 参数顺序 | 顺序相关；文档要求当链中有 resize 时 `format` 在最后 | 与顺序无关。`format,png/resize,w_100` 和 `resize,w_100/format,png` 产生逐位一致的输出，因为整个链被折叠为一个选项包，输出格式在保存时一次性决定。 |
| `quality,Q_100` | 绝对质量 | 支持。`q_` 也支持——参见[数值近似](#数值近似)。 |
| `heic` / `avif` 可用性 | 仅在部分区域 | 取决于构建；参见[构建依赖行为](#构建依赖行为)。 |

## `info`

携带分辨率元数据的图像的八字段格式与 OSS 文档示例逐位一致，包括键顺序。

| 情况 | OSS | iXoSS |
|---|---|---|
| 源文件**没有**分辨率元数据 | 文档中的无 EXIF 示例返回所有八个字段，`XResolution` 为 `1/1`，`ResolutionUnit` 为 `1` | 省略 `ResolutionUnit`、`XResolution` 和 `YResolution`，返回五个字段。**JPEG 通常不受影响**——JFIF `APP0` 算作分辨率元数据，因此普通 JPEG 仍然返回八个。没有任何信息的 PNG、WebP、GIF、TIFF 或 BMP 返回五个。 |
| PNG EXIF | 文档说明不解析 | 从 `eXIf` 块解析——带 EXIF 的 PNG 返回与 JPEG 相同的 25 个字段 |
| EXIF 值格式化 | exiv2 的美化打印机 | 原始 EXIF 类型：`FNumber` 是 `14/5` 而不是 `f/2.8`，`GPSLatitude` 是 `31/1 13/1 1236/25` 而不是 `31deg 13' 49.44"`，`ColorSpace` 是 `65535` |
| 没有标准名称的标签，以及 `UNDEFINED` 类型的标签如 `MakerNote` | 输出 | 跳过而不是作为十六进制转储 |
| EXIF `IFD1`（缩略图） | 其标签出现在 OSS 的示例中 | 遍历，`IFD0` 在两者共享的任何标签上胜出——相机在 `IFD1` 中写入 72 dpi，无论图像的实际分辨率是多少 |
| 动画 WebP 的 `FrameCount`，或超过 64 KiB 的 GIF | 真实计数 | 真实计数，从 libvips 获取。这两种情况需要完整文件读取，而 `info` 的其余部分只需 64 KiB 头部解析。 |
| APNG 的 `FrameCount` | — | 来自 `acTL` 块的真实计数，即使管线无法渲染这些帧——参见下文 |

> 分辨率字段行是这里一个前提**未经确认**的有意偏差。
> OSS 的页面没有说明任何方向的规则，其自身的无 EXIF 示例包含这三个字段。
> 省略它们的理由是示例图像作为 JPEG，几乎肯定携带了 JFIF 块。
> 如果你的客户端无条件读取 `XResolution`，这是需要重新审视的行。

## 动画

| 情况 | OSS | iXoSS |
|---|---|---|
| 动画输出格式 | — | 仅 WebP 和 GIF。`format,avif` 扁平化到第一帧，因为 libvips 只写入单帧 HEIF。 |
| 源超过帧数上限 | 仅受总像素限制 | `422 Source animation has N frames, limit is 300`。拒绝而不是截断：截断的动画格式正确但长度错误，调用者无法分辨。只有动画*输出*受影响——相同源请求 JPEG 是静态的，正常提供服务。 |
| APNG 请求为 `format,webp` 或 `format,gif` | — | `422 This build of libvips cannot read png animation`。任何静态格式、简单的 `resize` 和 `format,auto` 都会扁平化并正常提供服务。拒绝是运行时比较，不是版本检查，因此在任何能读取 APNG 帧的构建上自动停止。 |

## 数值近似

这些都无法与 OSS 逐位一致；它们是校准的，不是推导的。

| 参数 | 说明 |
|---|---|
| `quality,q_N` | OSS 的 `q_` 是*相对的*——源自身质量的 N%。iXoSS 将其视为绝对值。在接近 iXoSS 默认值时两者非常接近；在极低质量的源上，这里的 `q_` 比 OSS 生成更大的文件。`Q_` 在两者中都是绝对的。 |
| `sharpen`、`bright`、`contrast` | 映射到 libvips 操作，通过目视对比 OSS 输出进行校准 |
| `watermark` 文字阴影几何 | OSS 仅指定阴影的透明度；偏移和模糊在这里从字体大小缩放 |
| `watermark,type_` | 字体名称解析为哪个字体取决于主机的 fontconfig。安装 `font-wqy-zenhei` 以匹配 OSS 的默认字体。 |

## 限制和默认值

iXoSS 是自托管的，因此这些是配置而不是平台策略。
像素预算是有意与 OSS 匹配的；其余是 iXoSS 自己的。

| 限制 | OSS | iXoSS 默认值 | 环境变量 |
|---|---|---|---|
| 总像素数（`宽 × 高 × 帧数`） | 250,000,000 | 250,000,000 | `IXOSS_MAX_SRC_RESOLUTION` |
| 源单维度 | 30,000 px | 无限制；像素预算仍然适用 | — |
| 输出单维度 | — | 缩放以降低到配置的边界 | `IXOSS_MAX_RESULT_DIMENSION` |
| `rotate` 的单维度 | 4,096 px | 无限制——5000 px 的源可以正常旋转 | — |
| 动画帧数 | 仅像素预算 | 300 | `IXOSS_MAX_ANIMATION_FRAMES` |
| 源文件大小 | — | 100 MiB | `IXOSS_MAX_SOURCE_BYTES` |
| 水印像素数 | 未文档化 | 8 MP | `IXOSS_MAX_WATERMARK_RESOLUTION` |
| 水印文件大小 | 未文档化 | 16 MiB | `IXOSS_MAX_WATERMARK_BYTES` |
| 每请求处理时间 | — | 20 秒 | `IXOSS_PROCESS_TIMEOUT_MS` |

水印有自己的预算，而不是源图像的预算，而且严格得多：8 MP 对比源的 250 MP。
水印是合成到图像上的装饰，源上限对于装饰来说太宽松了——
一个 440 KB 的 PNG 解码为 144 MP，通过了它并在单次请求中消耗了 444 MB RSS。
`watermark,image_` 指定超过任一限制的对象返回 `413 SourceTooLarge`，
在头部检查时决定，在任何解码之前。如果部署确实需要合成大型内容，可以提高限制。

像素预算也适用于 `watermark,text_`，而 OSS 没有以任何文档化的方式进行约束。
`size_` 已经被语法限制在 1000，但文字本身没有，
因此长字符串在大尺寸下产生的画布不受任何约束：
`text_<30 chars>,size_1000` 在这里会被拒绝。`rotate_` 在旋转后再次检查，
因为 45° 旋转会使边界框增长最多一半。

像素预算设置为 OSS 的 250 MP 而不是更小的值，因此 OSS 会接受的源不会在这里被拒绝。
成本取决于请求，而不仅仅是源——在 17000×14000 的 JPEG（238 MP）上测量，
`IXOSS_CONCURRENCY=1`，单个请求的峰值 RSS：

| 请求 | 峰值 RSS |
|---|---|
| `resize,w_200` | 58 MiB——加载时缩小，完整帧永远不会被物化 |
| `format,jpg` | 787 MiB |
| `format,webp` | 1143 MiB |

因此缩略图工作负载接近零，而接近上限的全尺寸转码约消耗 1 GB，
乘以 `IXOSS_CONCURRENCY`。`IXOSS_MAX_SOURCE_BYTES`（100 MiB）约束文件但不约束像素数
——一个 238 MP 的纯色 JPEG 小于 4 MiB——因此它不能替代尺寸调整。

单维度行是剩余的差距：OSS 拒绝宽度或高度超过 30,000 px 的源，
`rotate` 为 4,096 px；iXoSS 都不强制执行，因此它接受 OSS 会拒绝的形状。

## 构建依赖行为

iXoSS 仅在 **Linux** 上受支持——这是 CI 构建和测试的平台，
也是唯一测量过这一切的平台。下面的行是关于 Linux 二进制文件链接的 libvips，
而不是关于 iXoSS。可编码集在启动时通过真实编码探测并记录在日志中：

```
level=INFO msg="encodable formats" formats="[jpg png webp gif avif jxl tiff bmp ico]"
```

| 情况 | 行为 |
|---|---|
| 此构建无法编码的格式 | `400 format "heic" cannot be produced by this build of libvips`，在任何工作开始之前 |
| TIFF 源 | 加载器的 `unlimited` 标志需要 libvips 8.17 配合 libtiff 4.7+ 构建。不存在时 libvips 保持自己的解码限制；TIFF 仍然加载。 |
| 动画 JPEG XL | 需要 libvips 8.16 才能读取；较旧的构建看到的是静态图像 |
| APNG | 没有已发布的 libvips 能读取 APNG 帧。它在 master 上，在未发布的 `8.19.0` 标题下，仅在 libpng 路径中且在 `PNG_APNG_SUPPORTED` 后面，因此未来的版本是必要但不充分的。参见上面的动画说明。 |

## 错误

信封匹配 OSS 的格式——`{"Code": "...", "Message": "..."}` 具有相同的 JSON 布局
——因此针对 OSS 编写的客户端可以解析 iXoSS 的失败。
`Code` 字符串本身**没有**与 OSS 的列表进行交叉检查；
iXoSS 发出 `NoSuchKey`、`InvalidArgument`、`InvalidImage`、`SourceTooLarge`、
`AccessDenied`、`MethodNotAllowed`、`RequestTimeout` 和 `InternalError`。

逃逸根目录的路径、指定目录的路径或不存在的路径都返回相同的 `404 NoSuchKey`，
这是有意的：区分它们会为探测者绘制文件系统地图。

---

## 已验证匹配

对照 OSS 文档检查并发现一致，列出以便读者知道它们被查看过而不是被忽略：

- 八个 `format` 值 `jpg` `png` `webp` `bmp` `gif` `tiff` `heic` `avif`
- `format,gif` 保持非 GIF 源为其自身格式
- JPEG 输出时透明度填充白色
- `quality,Q_100`
- `info` 字段格式、键顺序、`{"value": "..."}` 包装、全字符串值，
  以及 `Format` 中是 `jpg` 而不是 `jpeg`
- `average-hue` 返回裸 `0xRRGGBB` 纯文本，不是 JSON
- `info` 和 `average-hue` 是终止操作——都不能与变换链接

## 参考来源

- [对图片进行格式转换](https://help.aliyun.com/zh/oss/user-guide/convert-image-formats-2)
- [查询图片的EXIF信息](https://help.aliyun.com/zh/oss/user-guide/query-the-exif-data-of-an-image-4)
- [OSS图片处理常见问题及处理方法](https://help.aliyun.com/zh/oss/user-guide/faq-2)
