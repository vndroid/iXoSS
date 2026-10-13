# iXoSS

一个实现阿里云 OSS `x-oss-process` URL 语法的图像服务，基于 libvips 构建。
图像引擎移植自 [imgproxy](https://github.com/imgproxy/imgproxy)（Apache-2.0）；
详见 `NOTICE`。iXoSS 本身采用 AGPL-3.0 许可证——详见 `LICENSE`，
以及文末的许可证章节了解两者为何可以共存。

iXoSS 是一个独立项目，与阿里云无关。"OSS"仅指其实现的 `x-oss-process` URL 语法。

状态：**可用。** `resize`、`crop`、`indexcrop`、`trim`、`rotate`、
`auto-orient`、`blur`、`sharpen`、`pixelate`、`bright`、`contrast`、`circle`、
`rounded-corners`、`watermark`、`quality`、`format`、`interlace`、`info` 和
`average-hue` 通过 HTTP 从本地目录提供服务，
支持有界磁盘缓存、请求合并、
并发限制、基于 `Accept` 的格式协商以及可选的 HMAC 请求签名。

**从 OSS 迁移？** `DIFF.md` 是两者差异的完整清单：
缺少什么、新增了什么、哪些数值是近似值而非精确匹配，以及限制在哪里。

## 使用方法

```sh
IXOSS_ROOT=/srv/images \
IXOSS_CACHE_DIR=/var/cache/ixoss \
IXOSS_BIND=127.0.0.1:8080 \
  ixoss
```

```
GET /photo.jpg                                    原始文件，不做处理
GET /photo.jpg?x-oss-process=image/format,avif
GET /photo.jpg?x-oss-process=image/resize,w_800
GET /photo.jpg?x-oss-process=image/resize,m_fill,w_200,h_200/quality,q_80/format,avif
GET /photo.jpg?x-oss-process=image/info           JSON 元数据
GET /healthz
```

路径中包含重复斜杠的请求会收到 `308` 重定向到规范化 URL：`//` 变为 `/`，
`//sub//photo.jpg` 变为 `/sub/photo.jpg`。仅支持 `x-oss-process` 和
`x-ixoss-signature` 查询参数。不支持的查询参数和无效的处理选项会在任何路径
重定向之前返回 `400`。保留的查询键值对保持其原始顺序和 URL 编码，
包括可读的 `/` 和逗号。必要的转义如 `%26`、`%23` 和 `%2B` 保持不变。
格式错误的查询编码、未转义的分号或 URI 查询中的无效字符会返回 `400`。
签名覆盖的是重复斜杠已折叠的路径，因此重定向后签名仍然有效。
对于签名基于包含重复斜杠路径生成的旧 URL，需要重新签名。

### 响应头和条件请求

所有响应都包含 `X-Content-Type-Options: nosniff`。错误、健康检查和规范化重定向
使用 `Cache-Control: no-store`。成功的图像、元数据和平均色调响应默认为
`public, max-age=300`（五分钟），可通过 `IXOSS_CACHE_CONTROL` 配置。
对于内容哈希或版本化的 URL，可以显式配置
`public, max-age=31536000, immutable`。
之前缓存的响应会保留旧的有效期，直到过期或被清除；更改服务器策略不会
清除浏览器或 CDN 缓存。

原始文件和未变化的 resize/format 响应使用基于文件标识、大小和纳秒级
修改时间的弱 ETag；替换文件必须更新其修改时间。处理后的图像、info JSON
和平均色调结果使用实际响应字节的强 SHA-256 ETag。
GET 和 HEAD 接受 `If-None-Match`（包括弱标签、列表和 `*`），
当所选表示匹配时返回无 body 的 `304`。授权、参数验证和资源检查
在条件响应之前执行。`304` 保留 ETag、缓存策略，以及对于 `format,auto`
的 `Vary: Accept`。

需要处理且缓存未命中的 HEAD 请求仍然很轻量：
它不编码图像，也不生成 ETag 和输出长度。只有 `If-None-Match: *`
才能在现有源检查之后产生 `304`。缓存的 HEAD 使用与 GET 相同的内容 ETag。
条件请求节省响应带宽；未缓存的处理后的 GET 仍需要生成输出以计算其内容 ETag。

### 未变化图像请求

仅包含 `resize` 和/或 `format` 的处理链在所有请求的操作都保留源尺寸和格式时，
返回原始字节。此检查在磁盘缓存访问之前运行：它忽略已有的转换条目、
不编码或写入新条目，并报告 `X-IXoSS-Cache: BYPASS`。没有处理参数的原始请求
也报告相同的标记，包括 GET、HEAD 和条件 `304` 响应。

对于 700x500 的 JPEG，`image/resize,w_700`、`image/resize,w_700,h_500`、
`image/format,jpg` 和 `image/resize,w_700/format,jpg` 都会传输原始文件。
被默认 `limit_1` 规则阻止的 resize 也符合条件。`format,auto`
仅在其协商格式（或源回退）未变化时符合条件；它仍然发送 `Vary: Accept`。
`format,gif` 兼容规则继续保留非 GIF 源格式。

quality、crop、rotate、watermark 和所有其他操作保持正常的处理路径，
即使输出尺寸匹配。直通保留原始元数据、色彩配置、方向和动画字节，
而不是应用处理器的默认清理、质量或首选格式设置。
GET 和 HEAD 共享原始文件的弱 ETag 和长度，包括 `304` 条件响应。
参数/签名验证、源字节限制和已知源像素限制仍然适用。

尺寸通常来自文件头。没有受支持的头解析器的容器可能需要读取源文件并
加载元数据来确认尺寸；这不会转码图像。如果无法确认未变化的结果，
请求将继续通过常规处理路径。

### 支持的操作

**`resize`** — `m_` 模式、`w_`/`h_` 边界框、`l_` 最长边、`s_` 最短边、
`p_` 百分比、`limit_0|1`、`color_RRGGBB`。

| 模式 | 含义 |
|---|---|
| `lfit` *（默认）* | 适配到 w×h 内，保持宽高比 |
| `mfit` | 覆盖 w×h 不裁剪，因此一侧会溢出 |
| `fill` | 覆盖 w×h 然后居中裁剪到精确的 w×h |
| `pad` | 适配到 w×h 内，然后用 `color_`（默认白色）填充到精确的 w×h |
| `fixed` | 强制精确的 w×h，忽略宽高比 |

`limit_1` 是默认值，与 OSS 一致：如果执行请求会放大源图像，则返回未变化的源。
`limit_0` 允许放大。

**`crop`** — `w_`/`h_` 尺寸、`x_`/`y_` 偏移、`g_` 锚点
（`nw` *（默认）* `north` `ne` `west` `center` `east` `sw` `south` `se`）。
裁剪区域大于图像时会被限制到图像范围内，与 OSS 一致。

**`rotate`** — `0..360`，顺时针，与 OSS 一致。

90 的倍数是无损转置，保持画幅。其他角度是重采样：画布扩展到旋转矩形的
边界框，暴露的角落是透明的——或者当输出格式没有 alpha 通道时是背景色，
这与 OSS 的行为一致。300×200 的源在 45° 时输出为 354×354，
而 `rotate,45/format,jpg` 的角落是白色的。

两者使用不同的 libvips 调用，自由旋转在 resize 契约满足*之后*运行：
`resize,m_fill,w_100,h_100/rotate,45` 得到 141×141 的结果，
而不是一个被剪掉角落的 100×100 结果。

**`auto-orient`** — `0` 或 `1`：是否应用 EXIF 方向标签。默认 1，与 OSS 一致。

**`blur`** — `r_1..50`、`s_1..50`。两者都是必需的，与 OSS 一致，但只有 `s_`
（标准差）进入处理管线：libvips 的高斯模糊接受 sigma 并自行推导核半径，
因此 `r_` 被接受是为了 URL 兼容性，不会改变输出。

**`indexcrop`** — `x_<size>,i_<index>` 或 `y_<size>,i_<index>`。沿一个轴将图像
切成 `size` 像素的片段，保留第 `index` 个（从 0 开始）。尽管名字如此，
`x_`/`y_` 是切片的宽度/高度，而不是数量。最后一个短片段会被保留而不是丢弃，
与 OSS 一致。

**`sharpen`** — `50..399`。OSS 没有说明这个数字的含义；管线接受高斯 sigma，
因此这里映射为 `v/100`，将 OSS 推荐的 100 放在 sigma 1 的位置。
这是一个校准值，不是规范——输出不会与 OSS 逐位一致。

**`pixelate`** — `1..1000`，源像素中的块边长：`pixelate,8` 对每个 8×8 的块取平均值。
`1` 是无操作而不是错误，因此从缩放级别计算块大小的调用者不需要特殊处理最小值。

> `pixelate` 是 **iXoSS 新增操作**，不是 OSS 操作——OSS 的效果列表有
> blur、sharpen、bright 和 contrast，但没有 pixelate。它在这里是因为它是
> 唯一能可靠地使人脸或车牌不可读且不留可恢复原始数据的操作；
> 足够强的 blur 通常会毁掉画面的其余部分。

**`trim`** — `t_0..254` 阈值（默认 10）、`c_RRGGBB` 边框颜色、
`eh_0|1` / `ev_0|1` 等边裁剪。移除均匀的边框。

省略 `c_` 会从图像自身的边框检测颜色，这几乎总是你想要的；
指定它则只裁剪该颜色，其他颜色不受影响。`eh_1`/`ev_1` 从对边移除*相同*的量，
保持主体居中而不是贴边。

> `trim` 是 **iXoSS 新增操作**，不是 OSS 操作。它的价值在于修复一类源文件
> 而不是重新样式化：截图和导出的 logo 经常带有背景带，无论怎么缩放都无法去除，
> 在服务时裁剪可以避免重新切割原始文件。

**`bright`** — `-100..100`，`0` 不变。该值变为 0..255 刻度上的偏移量
（`v * 2.55`），因此 `bright,100` 是白色，`bright,-100` 是黑色。

**`contrast`** — `-100..100`，`0` 不变。通过标准对比度曲线
`259*(c+255) / (255*(259-c))` 映射，其中 `c = v * 2.55`：`-100` 坍缩为
平坦的中灰色，`100` 达到约 129 倍并充当阈值。简单的线性乘数会浪费正半部分
——加倍对比度几乎不可见，而有趣的值都在 4 倍以上。

> 两者都是校准值，不是规范：OSS 说明了范围但没有说明单位，
> 因此输出不会与 OSS 逐位一致。Alpha 保持不变，因此增亮透明 PNG
> 不会使其变为不透明。当两者同时给出时，它们被合并为**一次**线性运算，
> 这很重要：分开应用时，`contrast,-100/bright,50` 链会裁剪两次。

**`circle`** — `r_1..4096`。返回一个 `(2r+1)` 的正方形，包含内切圆。
超过图像可容纳的半径会被限制到最大的内切圆，与 OSS 一致——
300×200 的源使用 `circle,r_4096` 得到 199×199，即 `r = (min edge - 1) / 2`。

**`rounded-corners`** — `r_1..4096`。保持画幅并圆角；半径被限制到较短边的一半，
此时形状为体育场形。

> 两者都会从结果中裁掉透明像素，因此输出格式很重要：
> PNG 和 WebP 保留透明度，JPEG 获得背景色（默认白色）——
> OSS 的行为也相同。`circle/format,auto` 因此协商为支持 alpha 的格式
> 而不是 JPEG。已经部分透明的图像保持原样：掩码乘以现有的 alpha
> 而不是替换它。
>
> 掩码是算术的——一个用 libvips 运算评估的有符号距离场——而不是光栅化的形状，
> 因此不需要 librsvg，并且免费获得一像素抗锯齿弧线。

**`watermark`** — 图像、一行文字或两者并排。接受 OSS 文档中的所有参数。

| | |
|---|---|
| `image_` | 对象键的 base64url |
| `text_` | 文字的 base64url |
| `t_0..100` | 不透明度，默认 100 |
| `g_` | 锚点，默认 `se` |
| `x_`/`y_0..4096` | 距锚定边缘的偏移，默认 10 |
| `voffset_-1000..1000` | 距中心线的偏移，用于中间行锚点 |
| `P_1..100` | 将图像水印缩放到基础图像百分比 |
| `fill_0\|1` | 在整个图像上平铺水印 |
| `padx_`/`pady_0..4096` | 平铺之间的间距 |
| `type_` | 字体名称的 base64url，默认 `wqy-zenhei` |
| `color_RRGGBB` | 文字颜色，默认黑色 |
| `size_1..1000` | 文字大小（像素），默认 40 |
| `shadow_0..100` | 阴影不透明度，默认 0 |
| `rotate_0..360` | 旋转文字 |
| `order_0\|1` | 图像和文字的先后顺序，默认图像在前 |
| `align_0\|1\|2` | 顶部 / 中间 / 底部，默认底部 |
| `interval_0..1000` | 图像和文字之间的间距 |

对象键从与图像相同的根目录读取，通过相同的解析器，
因此路径遍历和符号链接逃逸在那里也会被拒绝。
接受所有四种 base64 拼写（原始/填充 × url/标准字母表）。

图像、文字和组合水印共享一个有界的内存 LRU（64 个条目，默认 64 MiB）。
文字水印的键覆盖所有渲染参数；相同的重复请求会复用它，
而任意新字符串只能驱逐较旧的条目。文字在 Pango 渲染之前会被转义，
因此请求中的标记无法覆盖已检查的字体大小。

无法全部满足的组合会被拒绝而不是半应用：
`y_` 与 `voffset_`（都是垂直偏移）、`g_`/`x_`/`y_` 与 `fill_1`（没有锚点）、
`padx_`/`pady_` 没有 `fill_1`、`P_` 没有图像、文字参数没有文字，
以及 `order_`/`align_`/`interval_` 没有两者。

> 关于文字有两点无法与 OSS 逐字节匹配。**字体名称解析为哪个字体**
> 取决于主机的 fontconfig，而不是我们的：OSS 的标识符被映射为带通用回退的
> 字体族列表，未列出的名称会被直接传递，以便部署可以请求其实际安装的字体。
> **阴影的几何形状**是校准值——OSS 的 `shadow_` 只给出透明度，
> 因此偏移和模糊在这里从字体大小缩放。

**`format`** — `jpg` `jpeg` `png` `webp` `gif` `avif` `heic` `jxl` `tiff` `bmp`，
以及 **`auto`**。

`format,gif` 遵循 OSS 的不寻常兼容规则：GIF 源保持 GIF，
而其他源保持自己的格式。它不会将 JPEG、PNG 或 WebP 量化为 GIF。

`format,auto` 是 iXoSS 新增操作，不是 OSS 操作。它选择客户端最高 `q` 值的
可接受、可用格式——AVIF 在平局时优先于 WebP——然后回退到源自身的格式，
并设置 `Vary: Accept`。它的存在是因为替代方案是在每个调用点手写一个
带有 AVIF `<source>` 和回退的 `<picture>` 元素，
并且在浏览器支持变化时立即过时。

通配符被故意忽略：每个浏览器都发送 `*/*`，将其视为"AVIF 可以"
会向无法读取 AVIF 的客户端发送 AVIF。JPEG XL 不是候选者——
在约 15% 的支持率下，这意味着编码一种几乎没人能读取的格式，
而且它是三者中生成最慢的。需要时请显式请求。

**`quality`** — `Q_1..100`（绝对）或 `q_1..100`。

> **`q_` 是近似值。** 在 OSS 中，`q_n` 是*相对的*：对于 JPEG，它意味着源自身
> 质量的 n%，而 `Q_n` 是绝对的。恢复源的质量意味着读回其量化表，
> libvips 不暴露这个信息，而拒绝 `q_` 会破坏大多数 OSS URL 使用的拼写。
> 因此 iXoSS 将两者都视为绝对值。对于以 iXoSS 默认质量保存的源，
> 两者非常接近；对于重度压缩的源，这里的 `q_n` 生成的文件比 OSS 大。

**`interlace`** — `0` 或 `1`。`1` 编码结果使浏览器可以在下载时逐步绘制。

OSS 仅对 JPEG 文档化了此项，意为渐进扫描顺序。
iXoSS 也将其应用于 PNG，其等价物是 Adam7 隔行扫描：
调用者要求的是相同行为，编码器也支持。对于任何其他输出格式，它不起作用。
未设置时，应用进程范围的 `IXOSS_JPEG_PROGRESSIVE` / `IXOSS_PNG_INTERLACED` 设置
——该操作按请求覆盖它们，而不是替换它们。

**`info`** — 终止操作，不能与变换链接（与 OSS 规则相同）。

`image/info` 返回 OSS 格式，每个值都是字符串：

```json
{
  "FileSize": {"value": "17947"},
  "Format": {"value": "jpg"},
  "FrameCount": {"value": "1"},
  "ImageHeight": {"value": "267"},
  "ImageWidth": {"value": "400"}
}
```

当源文件包含 EXIF 时，其标签会合并到同一对象中，与 OSS 一致——
`Make`、`Model`、`DateTime`、`Orientation`、`LensModel`、GPS 块等。
标签直接来自 JPEG 的 `APP1`、PNG 的 `eXIf` 块和 WebP 的 `EXIF` 块；
对于 AVIF、HEIC、JXL 和 TIFF，它们来自 libvips，
无论容器如何包装，它都返回相同的裸 TIFF 块。
上述五个字段是无条件的。`ResolutionUnit`、`XResolution` 和 `YResolution`
仅在源文件实际携带 JFIF、EXIF 或 PNG `pHYs` 分辨率元数据时添加。
解析 PNG EXIF 是 iXoSS 的扩展；OSS 目前文档化了 PNG 作为受支持的 info 容器，
但不暴露其 EXIF 标签。

> 两个有意的差异，都是关于*格式化*而不是出现哪些标签。
> 值从原始 EXIF 类型渲染——`GPSLatitude` 是 `"39/1 54/1 2668/100"`，
> 而 OSS 通过 exiv2 的美化打印机处理相同标签，输出 `"39deg 54' 26.68\""`。
> 没有标准名称的标签以及 `UNDEFINED` 类型的标签（如 `MakerNote`）
> 会被跳过，而不是作为十六进制块发出。

`info` 描述的是*源对象*，因此 `FrameCount` 是文件自身的帧数，
即使管线无法产出那么多——参见下面的 APNG 说明，
其中会拒绝需要扁平化的变换。

`info` 尽可能从文件的前 64 KiB 回答——这就是该端点的意义所在，
也是它比获取图像便宜得多的原因。有三种情况无法这样回答，
需要回退到 libvips（读取整个文件）：

- 没有头部遍历的容器：AVIF、HEIC、JXL、TIFF、BMP；
- JPEG 的 `SOF` 标记在 64 KiB 之后——大型 EXIF 缩略图加上分段 ICC 配置就足够，
  不需要相机做任何异常操作就能产生这种情况；
- 动画 WebP，或其块超出窗口的 GIF，其中几何信息已知但帧数未知。
  WebP 根本没有帧数字段：帧是 `ANMF` 块，因此计数需要遍历文件。

由于该回退会读取所有内容，`IXOSS_MAX_SOURCE_BYTES` 适用于它，
超过限制的源会收到 `413`——与其变换得到的响应相同。
限制在读取时检查，而不是在请求时检查，
因此头部遍历可以回答的大型 JPEG 仍然会被提供。

### 动画

动画源通过 `resize`、`crop`、`watermark` 等保持其动画，
与 OSS 一致，只要输出格式可以容纳动画。两种格式可以：WebP 和 GIF。
其他都是静态的，因此 `format,jpg` 和 `format,avif` 会扁平化到第一帧——
包括 AVIF，因为 libvips 只写入单帧 HEIF。

`IXOSS_MAX_ANIMATION_FRAMES` 将长度限制为 300 帧，超过的源会被**拒绝**并返回 `422`，
同时说明两个数字：

```
{"Code":"InvalidImage","Message":"Source animation has 400 frames, limit is 300"}
```

拒绝而不是截断是有意的。截断是以前做的——加载前 N 帧然后继续——
它产生了一个格式正确但长度错误的动画，且没有任何提示，
这正是调用者无法检测到的结果。`image/info` 报告源的真实帧数，
因此截断也会导致两个端点相互矛盾。只有动画*输出*受影响：
同样的超长 GIF 如果请求 JPEG 或 AVIF 则是静态的，正常提供服务。

> 默认值与 imgproxy 不同，后者是 `1`——"永不动画"。
> 对于指向开放互联网的代理来说这是合理的默认值，但在这里是错误的。
> 提高它不会放松任何东西：真正的预算是 `宽 x 高 x 帧数` 对比
> `IXOSS_MAX_SRC_RESOLUTION`，与 OSS 限制相同的乘积，现在也是 250 MP。
> 在 300 帧时，这会拒绝每帧超过约 912x912 的任何内容，无论帧数上限如何。
> 帧数上限管理的是像素数无法看到的每帧开销——一个小型的、无尽的动画。

一个值得知道的意外：输出可能比源有**更少**的帧，而没有丢弃任何内容。
libwebp 将与前一帧相同的帧合并到其中并延长该帧的持续时间。
一个 300 帧的 GIF，其调色板量化使某些相邻帧相同，在这里输出为 294 帧的 WebP
——两者都是 12,000 毫秒长，视觉上是相同的动画。

#### APNG

动画 PNG 是一个特殊情况，因为 iXoSS 的两个部分对此有分歧。
`image/info` 背后的头部遍历读取 `acTL` 块，因此 info 报告真实帧数。
没有已发布的 libvips 能读取 APNG 帧，因此它加载默认图像并报告一页。

因此，指定一个本可以承载动画的格式会被拒绝，
而不是悄悄返回一个静态图像：

```
GET /a.png?x-oss-process=image/format,webp
→ 422 This build of libvips cannot read png animation; the source has 3 frames.
      Request a still format instead.
```

该文件的其他所有内容都照常提供——`format,jpg`、`format,png`、`format,avif`、
简单的 `resize`，以及任何 `Accept` 下的 `format,auto`。
区别在于调用者是否*请求*了可以承载动画的格式。`format,auto` 委托选择，
因此用静态图像回答是服务器的决定；用 422 回答浏览器的内容协商则不是。

该检查是一个比较——容器声称的与 libvips 加载的——而不是格式列表或版本测试。
因此它不需要维护：在能读取 APNG 的 libvips 上，两个数字一致，
拒绝自动停止触发，普通的动画路径接管。

**`average-hue`** — 终止操作，与 `info` 一样，响应是**纯文本**，
不是 JSON，因为 OSS 返回的就是这样：

```
0x285ac8
```

与 `info` 不同，这需要像素数据，因此它会解码图像——一次解码，
没有缩放也没有编码，只为一个七字节的 body。只读取动画的第一帧。

> **透明度是加权的，而不是被忽略的。** 透明背景上的 logo 存储为
> 大部分透明的*黑色*，对这些像素的简单平均会对一个人称为绿色的图像回答"黑色"。
> iXoSS 使用预乘并除以 alpha 总和，因此透明背景上的绿色标记报告为绿色。
> 对于完全不透明的图像，两者是相同的，因此这只会改变简单平均出错时的结果。

错误使用 OSS 信封格式，因此针对 OSS 编写的客户端可以不变地解析它们：

```json
{"Code": "InvalidArgument", "Message": "unsupported format \"tga\""}
```

### 配置

| 变量 | 默认值 | 含义 |
|---|---|---|
| `IXOSS_ROOT` | *（必需）* | 图像服务的目录 |
| `IXOSS_CACHE_DIR` | *（未设置——无缓存）* | 转码结果的存储位置 |
| `IXOSS_STYLES` | *（未设置）* | 将样式名映射到 `image/...` 链的 JSON 文件；启动时加载和验证 |
| `IXOSS_PASSTHROUGH_NON_IMAGES` | `false` | 允许原始对象端点提供非注册图像类型的字节；否则返回 `415` |
| `IXOSS_BIND` | `:8080` | 监听地址 |
| `IXOSS_CONCURRENCY` | `GOMAXPROCS` | 同时编码数 |
| `IXOSS_MAX_SOURCE_BYTES` | `104857600` | 拒绝更大的源文件 |
| `IXOSS_PROCESS_TIMEOUT_MS` | `20000` | 每请求处理截止时间 |
| `IXOSS_CACHE_CONTROL` | `public, max-age=300` | 成功响应的缓存策略 |
| `IXOSS_CACHE_MAX_BYTES` | `0` *（无限制）* | 超过此值时驱逐最近最少使用的条目 |
| `IXOSS_CACHE_MAX_AGE_HOURS` | `0` *（无时间限制）* | 驱逐超过此时间未访问的条目 |
| `IXOSS_CACHE_EVICT_INTERVAL_MIN` | `10` | 清扫频率 |
| `IXOSS_AVIF_SPEED` | `8` | 0 最慢/最小 .. 9 最快/最大 |
| `IXOSS_MAX_ANIMATION_FRAMES` | `300` | 拒绝超过此长度的动画 |
| `IXOSS_MAX_WATERMARK_RESOLUTION` | `8` *（百万像素）* | 拒绝超过此像素数的水印——参见[水印限制](#水印限制) |
| `IXOSS_MAX_WATERMARK_BYTES` | `16777216` | 拒绝线路上大于此值的水印 |
| `IXOSS_WATERMARK_CACHE_BYTES` | `67108864` | 水印缓存可持有的总内存 |
| `IXOSS_KEY` | *（未设置——无签名）* | 十六进制 HMAC 密钥，逗号分隔用于轮换 |
| `IXOSS_SALT` | *（未设置——无签名）* | 十六进制 HMAC 盐，每个密钥一个 |
| `IXOSS_SIGNATURE_SIZE` | `32` | 保留的 HMAC 字节数，1..32 |
| `IXOSS_TRUSTED_SIGNATURES` | *（未设置）* | 原样接受的签名字符串 |
| `IXOSS_LOG_LEVEL` | `info` | debug / info / warn / error |

`IXOSS_ROOT` 是必需的，必须指定一个已存在的目录。

保存的样式使用 `?x-oss-process=style/thumb`（不与其他操作组合）。
例如，设置 `IXOSS_STYLES=/etc/ixoss/styles.json` 并在文件中放入：

```json
{"thumb":"image/resize,w_200/quality,q_80","hero":"image/resize,w_1600/format,auto"}
```

名称包含 1–63 个 ASCII 字母、数字、`_`、`-` 或 `.`。定义不能引用另一个样式。
无效定义会阻止启动；编辑在重启后生效。缓存条目按展开后的链作为键，
因此更改样式不会复用其之前的输出。签名 URL 仍然签名公共别名
（`style/thumb`）：更改定义也会改变现有签名 URL 请求的内容。
外部 CDN 或浏览器仍然可以在同一 URL 下保留旧响应，
直到其缓存有效期到期。更改定义时使用版本化的样式名
（例如 `thumb-v2`）或清除外部缓存。

### 水印限制

水印是合成到图像上的装饰，它有自己的预算而不是源图像的预算。
三个限制，因为它们阻止三种不同的问题：

| 变量 | 约束 | 为什么不是其他 |
|---|---|---|
| `IXOSS_MAX_WATERMARK_RESOLUTION` | 单个水印，以像素计 | **保护进程的那个。** 字节和像素几乎没有关系：一个 440 KB 的纯色 PNG 解码后为 144 百万像素，一次请求实测消耗 444 MB RSS。没有足够低的字节限制能捕获它，而源图像自身的 250 MP 预算——对照片合适，对 logo 来说荒谬——会让它通过。 |
| `IXOSS_MAX_WATERMARK_BYTES` | 单个水印，在线路上 | 廉价的检查；不需要解码。 |
| `IXOSS_WATERMARK_CACHE_BYTES` | 所有保留的水印合计 | 单项限制约束一个条目，条目上限约束数量。它们的乘积才是实际驻留的内存，64 个条目在源的 100 MiB 限制下达到 6.25 GiB。 |

默认值为 8 MP、16 MiB 和 64 MiB，加上固定的 64 个保留水印上限。
8 MP 是 2828×2828 的正方形或 4000×2000 的横幅。成本大约按每百万像素 3 MB RSS 缩放，
因此默认值约为每个并发使用 25 MB——提高之前乘以 `IXOSS_CONCURRENCY`。
超过任何这些限制的水印会收到 `413 SourceTooLarge`，在头部检查时决定，
在任何解码之前。

像素预算也覆盖 `text_`，而且必须如此：`text_`、`size_` 和 `rotate_`
是免费的请求参数，因此文字水印产生的画布不受任何文件约束。
`text_<30 chars>,size_1000` 曾经被提供服务，消耗 350 MB RSS，
而 `text_<20 chars>,size_1000,rotate_45` 在花费 598 MB *之后*才被管线拒绝
——一个拒绝成本比它拒绝的工作还要高的限制不是限制。
现在预算在渲染之前（从画布的故意低估计算，因此它永远不会拒绝本可以适应的文字）、
渲染之后以及任何旋转之后再次应用，因为旋转 45° 会使边界框增长最多一半。
通过估计但未通过实际预算的文字仍然会被渲染后再拒绝；
该残留受 Pango 自身的表面限制约 32767 px 约束。

如果 `IXOSS_WATERMARK_CACHE_BYTES` 设置低于 `IXOSS_MAX_WATERMARK_BYTES`，
单项限制会降低到匹配它，服务器会在启动时说明：
总量是内存约束，而有人故意设置的内存约束不应该被单项默认值悄悄提高。

对同一水印的并发首次请求会合并为一次获取，
因此冷缓存和流量突发只花费一次下载而不是每次请求一次。

imgproxy 文档中作为 `IMGPROXY_*` 的每个 `IXOSS_*` 变量，
用于处理和安全层的，同样适用；只有前缀变了。

### 请求签名

默认关闭。设置 **`IXOSS_KEY`** 和 **`IXOSS_SALT`**（十六进制编码）后，
除 `/healthz` 外的每个请求都必须携带有效的 `x-ixoss-signature`，
否则返回 `403` 和 `{"Code":"AccessDenied","Message":"Forbidden"}`。
任一未设置时，不检查任何内容，服务器在启动时说明一次。

未签名的实例允许任何能访问端口的人变换 `IXOSS_ROOT` 下的任何对象。
需要限制访问时配置签名。

```sh
IXOSS_KEY=$(openssl rand -hex 32) IXOSS_SALT=$(openssl rand -hex 32) ixoss
```

签名内容是重复斜杠已折叠的解码对象路径，
后跟未变化的解码处理查询值：

```
/photo.jpg
/photo.jpg?x-oss-process=image/resize,w_800
```

两部分都必须包含。仅签名路径会允许任何人重写他们获得的 URL 上的变换，
而无限制的 resize 链是花费他人 CPU 的廉价方式；仅签名链会允许他们
将其指向不同的对象。其他查询参数在签名之外，并在规范化 URL 重定向时被移除。

`cmd/sign` 计算签名，读取相同的两个变量：

```sh
go build -o sign ./cmd/sign
SIG=$(IXOSS_KEY=$KEY IXOSS_SALT=$SALT ./sign '/photo.jpg?x-oss-process=image/resize,w_800')
curl "http://localhost:8080/photo.jpg?x-oss-process=image/resize,w_800&x-ixoss-signature=$SIG"
```

链必须逐字节匹配，包括其自身参数的顺序，因为该字符串就是被签名的内容。

两个值得知道的细节。`IXOSS_KEY` 和 `IXOSS_SALT` 是逗号分隔的列表，
**任何**配置的配对都可以验证，因此可以在旧密钥退役之前部署新密钥。
而且拒绝 body 对于缺少签名、错误密钥和为其他路径签发的签名是故意相同的
——真正的原因在 debug 级别的日志中，而不是给客户端。

imgproxy 在 URL 路径中签名，因为其路径*就是*请求。iXoSS 的路径是对象键，
没有其他含义，因此签名是查询参数而不是插入到真实文件名前面的段。

## 为什么不直接运行 imgproxy

imgproxy 已经能很好地完成转码，但有两点不合适：

- 它的 URL 语法是自己的，不是 `x-oss-process`
- 它的 `/info` 端点是 **Pro（付费）** 功能

因此 iXoSS 保留了 imgproxy 的 libvips 层——这是花了多年才做对的部分——
并替换了 URL 前端和 info 端点。

## 移植了什么，留下了什么

整个 imgproxy `processing` 包都在这里——resize、crop、gravity、trim、
padding、extend、watermark、filters、色彩管理、元数据剥离。
没有随之而来的是依赖树。

| | 内部包 | Go 代码行数 | 第三方模块 |
|---|---:|---:|---:|
| imgproxy 的 `processing` 闭包 | 56 | 25,271 | 25 |
| **iXoSS** | **19** | **8,208** | **1** |

这 25 个模块包括 AWS SDK、Azure SDK、Google Cloud Storage、OpenStack Swift、
OpenTelemetry、Datadog、New Relic、Sentry、Airbrake、Bugsnag、Prometheus 和 gopsutil。
它们都不是处理图像所必需的。它们恰好沿两条边传递到达：

    processing -> server    -> monitoring -> 每个可观测性后端
    processing -> imagedata -> fetcher    -> storage  -> 每个云后端

切断这两条边，整个就坍缩了。`processing` 在三个调用点接触 `server`，
都是 `server.CheckTimeout(ctx)`；现在变成了 `internal/timeout`。
`imagedata` 被精简重写了。剩下的一个模块是 `github.com/trimmer-io/go-xmp`，
`processing` 确实使用它来保留 XMP 元数据。

### 从 imgproxy 移植的部分

维护的移植版本锁定在 **v4.0.17 审计/安全基线**
（`2c89e4ebff3384caf6752db2ec861b4ef5fe675c`），包含 iXoSS 修改和
精选的 master 更改。原始导入未记录其确切版本。
参见[上游审计](third_party/imgproxy/README.md)和
[版本锁定](third_party/imgproxy/UPSTREAM.json)了解来源、文件哈希和回溯的安全修复。
此标记与 iXoSS 的版本分开。

```
internal/processing/     完整的管线
internal/options/        用作管线 IR 的类型化选项包
internal/security/       源图像限制（最大分辨率、最大帧数等）
internal/vips/           libvips cgo 绑定（1,307 Go + 3,770 C）
internal/imagetype/      格式注册表和魔数字节检测
internal/imagemeta/      IPTC 和 Photoshop 元数据块
internal/auximageprovider/  水印源
internal/env/            类型化环境变量描述符
internal/bufreader/  internal/errctx/  internal/ioutil/  internal/imath/  internal/ensure/
```

### 为 iXoSS 编写的部分

```
internal/imagedata/      引用计数的 bytes+format 持有者
internal/timeout/        iXoSS 从 imgproxy 的 server 包中需要的唯一函数
```

### 相对于 imgproxy 的更改

1. **`imagedata` 精简重写。** 保留：引用计数和取消钩子——不是可选的，
   因为 `vips.Image.Save()` 返回一个由 `VipsTarget` 拥有的缓冲区，
   附加的取消函数是唯一释放它的东西。移除：HTTP 下载、响应限制、
   流式提供者，以及它们背后的云存储传输。

2. **`vips` 不再导入 `imagedata` 和 `options`。** 耦合点是 8 个调用点。
   `vips.Image.Save()` 失去了其 `*options.Options` 参数，
   imgproxy 声明了但从未读取（`newSaveOptions(_ *options.Options)`）。

3. **`env` 移除了 `aws.go`、`gcp.go`、`load.go`。** 这些从 AWS Secrets Manager、
   AWS SSM 和 GCP Secret Manager 解析配置值，是图中出现三个云 SDK 的唯一原因。
   类型化描述符核心未变。

4. **`server.CheckTimeout` → `internal/timeout.Check`。** 三个调用点。

5. **`options/parser` 移除。** 那是 imgproxy 自己的 URL 语法；
   iXoSS 改为解析 `x-oss-process`。这也移除了 `clientfeatures`。

6. **SVG 移除** — `imagetype/svg.go`（检测）和 `processing/svg`（清理）都移除。
   两者都需要 imgproxy 的 `xmlparser`，后者需要 `golang.org/x/net/html/charset`。
   iXoSS 不光栅化或提供矢量图，*不清理*就传递 SVG 比拒绝更糟糕，
   因此 `skipStandardProcessing` 对 SVG 输入返回明确的错误。

7. **水印仅从磁盘加载。** imgproxy 的 `NewStaticProvider` 也接受 URL 并下载，
   这正是拉入 `imagedata.Factory` 和 fetcher/storage 树的原因。

8. **环境变量前缀 `IMGPROXY_*` → `IXOSS_*`。** 移除了未使用的源 URL 白名单
   及其 `IXOSS_ALLOWED_SOURCES` 配置；源对象仅在 `IXOSS_ROOT` 下解析。

9. **测试未移植。** 它们依赖 `testify` 和大型 `testdata` 树。

10. **cgo 层新增六个操作**，在 `internal/vips/vips.c` 中，
    绑定在 `vips.go` 中。imgproxy 没有这些。

    | | |
    |---|---|
    | `vips_linear_go` | 色彩通道的线性变换，alpha 不变——`bright`、`contrast` |
    | `vips_round_corners_go` | 圆角矩形 alpha 掩码——`circle`、`rounded-corners` |
    | `vips_rotate_go` | 任意角度旋转，透明背景——`rotate` |
    | `vips_text_go` | Pango 文字作为彩色 RGBA 图像，带可选阴影——`watermark,text_` |
    | `vips_ensure_alpha_go` | 添加不透明 alpha 通道，使嵌入留下透明边距 |
    | `vips_average_hue_go` | 一次遍历的每通道均值，alpha 加权——`average-hue` |

    三个匹配的管线步骤——`processing.adjust`、`processing.rotateFree`
    和 `processing.roundCorners`——位于 `cropToResult` 和 `fixSize` 之间。

11. **`internal/imageinfo` 新增 EXIF 读取。** imgproxy 没有 info 端点；
    回答 `image/info` 的 TIFF/IFD 遍历是 iXoSS 的。

12. **`vips.Image.Save` 新增 `SaveOverrides` 参数。** imgproxy 的保存选项都是
    进程范围的；OSS 的 `interlace` 是按 URL 的。覆盖结构体只携带这一个字段，
    因此 `internal/vips` 仍然不导入它在 (2) 中解耦的选项包。

## 构建要求

**仅限 Linux。** 这就是全部的支持声明：Linux 是 CI 构建和测试的平台，
下面 Dockerfile 产出的平台，也是唯一测量过这一切的平台。
这里没有故意对另一个 Unix 不友好——代码是普通的 cgo，
构建标签诚实地说明它们覆盖哪个平台——但如果在 macOS 或 BSD 上
不能构建或不能正常工作，那不是 iXoSS 的 bug。
下面的 macOS 说明是用于开发代码，而不是运行它。

- Go 1.26+
- libvips 8.13+，至少需要：libjpeg、libpng、libwebp（8.16+ 仅在需要*读取*
  动画 JPEG XL 时需要——参见"尚未构建"）
- TIFF 在上述任一版本上加载。在 libvips 8.17 配合 libtiff 4.7+ 构建时，
  加载器的 `unlimited` 标志按配置传递；在更旧的版本上该标志不存在，
  libvips 保持自己的解码限制，这是安全的方向。你拥有哪个版本由
  `go test ./internal/vips` 在日志中说明
- 对于 `watermark,text_`：libvips 需要配合 Pango 构建，加上主机上安装的字体
  ——如果文字将是中文，需要 CJK 字体，否则 Pango 会渲染方块
- 对于 AVIF 输出：libheif 需要配合 AV1 **编码器**构建（aom、SVT-AV1 或 rav1e）
- 对于 JPEG XL 输出：libjxl

Alpine 3.23（`vips` 8.17.3、`libheif` 1.23.0）：

```sh
apk add --no-cache \
  build-base pkgconf \
  vips-dev vips-heif vips-jxl \
  font-wqy-zenhei font-noto-cjk
```

Alpine 自带的 `go` 包是 1.25.10——比本模块要求的 1.26 旧，
且没有后续补丁版本的标准库安全修复。CI 和下面的镜像使用官方的
`golang:1.26-alpine3.23` 镜像构建，其标签浮动到最新的 1.26.x；
在 Docker 之外，从 go.dev 安装 Go 而不是从 apk。

仅运行时，不含工具链：

```sh
apk add --no-cache vips vips-heif vips-jxl font-wqy-zenhei
```

**`vips-heif` 和 `vips-jxl` 不是可选的。** Alpine 将 libvips 与这两个编解码器
构建为各自包中的*动态模块*，因此仅 `vips-dev` 给出的 libvips 能通过启动
然后在第一个 `format,avif` 请求时失败，报"cannot be produced by this build of libvips"。

构建实际可以编码哪些格式在启动时通过用每种格式编码一个 32x32 图像来确定，
并记录在日志中：

```
level=INFO msg="encodable formats" formats="[jpg png webp gif avif jxl tiff bmp ico]"
```

检查操作表不够好，AVIF 正是失败的地方。libvips 有一个 `heifsave`；
它将像素交给 libheif，后者分派到由 `compression` 选择的编解码器插件。
有 libheif 但没有 AV1 编码器的构建——Debian 和 Ubuntu 的默认配置——
*有* `heifsave_target`，因此表说是的，请求通过验证，
编码在管线深处失败为 `500`，没有有用的消息。
探测所有十种格式花费约 25 毫秒的启动时间，并将其转变为命名格式的 `400`。

另一方面，AVIF 编码器不需要额外的东西：Alpine 的 libheif 是针对 `aom-dev`
构建的，没有插件标志，因此 aomenc 被链接进去——不像 Ubuntu，那里它是一个单独的包。

字体仅对 `watermark,text_` 重要。`font-wqy-zenhei` 值得优先安装，
因为它是 OSS 默认命名的字体，因此 `type_` 解析为 iXoSS 映射期望的同一字体；
`font-noto-cjk` 是一个大包，如果文字不会是中文则可以省略。

构建仓库的 [Dockerfile](Dockerfile)，使用 Go 1.26 在 Alpine 3.23 上编译，
Alpine 3.23 作为运行时：

```sh
docker build -t ixoss:alpine3.23 .
mkdir -p images
docker run --rm -p 8080:8080 \
  --cap-add=SYS_NICE \
  -v "$PWD/images:/srv/images:ro" \
  --mount type=volume,source=ixoss-cache,target=/var/cache/ixoss \
  ixoss:alpine3.23
```

镜像包含 AVIF 和 JPEG XL 模块、中文水印字体和许可证声明。
它以 UID/GID 10001 运行；源文件必须对该用户可读。默认磁盘缓存位于
`/var/cache/ixoss`；如果你绑定挂载缓存目录而不是使用上面的命名卷，
请使其可被 UID 10001 写入。使用 Docker 的 `-e IXOSS_...` 选项覆盖配置。
构建上下文仅包含 Go 源文件、模块文件和许可证声明。

或使用 [Compose 示例](compose.example.yaml)，包含持久缓存卷、健康检查和自动重启。
它每 10 分钟检查缓存，执行 1 GiB 大小限制并移除 7 天未访问的条目。
调整示例中的 `IXOSS_CACHE_*` 环境变量来更改这些限制：

```sh
mkdir -p images
docker compose -f compose.example.yaml up -d --build
```

两个示例都添加了 `SYS_NICE` 以允许 HEIC 编码器的 NUMA 内存策略调用，
避免启动时出现 `set_mempolicy: Operation not permitted` 消息。

Ubuntu 24.04：

```sh
apt-get install -y libvips-dev libheif-plugin-aomenc libheif-plugin-dav1d
```

Ubuntu 上自带的 `libvips-dev` 只有 HEIF *解码*；没有 `libheif-plugin-aomenc`，
AVIF 保存会失败并报 `heifsave: Unsupported compression`。

macOS——仅用于本地开发，未经测试且不支持作为目标平台：

```sh
brew install vips        # 包含带 aom 的 libheif 和 libjxl
```

在信任构建之前验证编码器确实存在：

```sh
vips copy input.png /tmp/probe.avif && echo AVIF ok
vips copy input.png /tmp/probe.jxl  && echo JXL ok
```

## 实测数据

`cmd/smoke` 将一张图像通过完整管线运行。在 2 vCPU 上，一张 2400×1350 的照片
（源 JPEG q85，130,340 字节）：

```
选项                     输出尺寸        字节      耗时
format,avif            2400x1350     31,697    149 ms
format,jxl             2400x1350     63,198    546 ms
format,webp            2400x1350     45,402    221 ms
format,jpg             2400x1350    102,136     18 ms
resize w=800 + avif    800x450       11,637     58 ms
```

最后一行不在下面的路线图中——resize 随移植一起到来且已经工作。
只缺少它的 URL 语法。

AVIF 速度由 `IXOSS_AVIF_SPEED` 控制（0 最慢/最小 .. 9 最快/最大，默认 8）。
在同一台机器和图像上用 `avifenc` 测量，作为校准：

```
-s 0   7.56 s   22 KB
-s 4   2.08 s   24 KB
-s 6   0.38 s   25 KB     <- 大小/时间拐点
-s 8   0.19 s   41 KB
-s 10  0.14 s   63 KB
```

每个转换结果必须被缓存；在 149 毫秒下，一页缩略图否则会饱和一个小盒子。

## 端到端验证

针对运行中的服务器，源文件 `t.jpg` 2400×1350：

```
info (jpg/png/gif/webp)     尺寸与 Pillow 完全匹配；GIF 帧数也匹配
info (avif)                 通过 libvips 回退得到 2400x1350
format,avif|jxl|webp|png|jpg  200，正确的 Content-Type，解码为 2400x1350
cache                       MISS 223ms -> HIT 1.4ms
singleflight                12 个并发相同请求 -> 1 MISS + 11 COALESCED，
                            1 个缓存条目（不是 12 次编码）
HEAD                        200，无 body；POST -> 405
```

Resize，针对 400×300 源文件，检查解码输出而不仅是状态码：

```
m_lfit,w_200,h_200            -> 200x150     m_mfit,w_200,h_200   -> 267x200
m_fill,w_200,h_200            -> 200x200     m_pad,w_200,h_200    -> 200x200
m_fixed,w_200,h_200           -> 200x200     w_200                -> 200x150
h_150                         -> 200x150     l_200                -> 200x150
s_150                         -> 200x150     p_50                 -> 200x150
p_200                         -> 400x300     p_200,limit_0        -> 800x600
m_lfit,w_800,h_600            -> 400x300     ...,limit_0          -> 800x600
w_200/format,avif             -> 200x150 image/avif
```

两个 `limit` 行是 OSS 默认值的效果：请求比源更大的内容返回未变化的源。

Pad 用请求的颜色填充（`color_FF8000` 给出 `(255,127,0)` 边框），
质量是单调的：`Q_30` 7,423 B、`Q_60` 11,016 B、`Q_90` 30,425 B。

Crop 和 rotate 通过像素检查，不是通过状态码，针对一个四象限着色的 400×300 图像
——左上红色、右上绿色、左下蓝色、右下黄色：

```
crop,w_200,h_150             -> 全红        （nw 是默认锚点）
crop,w_200,h_150,g_ne        -> 全绿
crop,w_200,h_150,g_sw        -> 全蓝
crop,w_200,h_150,g_se        -> 全黄
crop,w_200,h_150,x_200       -> 全绿         （x_/y_ 是绝对像素）
crop,w_200,h_150,y_150       -> 全蓝
crop,w_200,h_150,x_200,y_150 -> 全黄
crop,w_200,h_150,g_center    -> 四个象限都有，因为中心框横跨它们

rotate,90   300x400，红色从左上移到右上    （顺时针，与 OSS 一致）
rotate,180  400x300，红色从左上移到右下
rotate,270  300x400，红色从左上移到左下
```

Blur 是单调的，以硬象限边界上的色差衡量：未模糊 510、`s_8` 160、`s_25` 52。

完整链——`crop,w_200,h_150,g_se/resize,w_100/format,avif`——返回一个
100×75 的 AVIF，581 字节，中心像素为 `(255,255,2)`，即黄色象限，缩放并重新编码。

对同一 400×300 四象限图像的 `indexcrop`：

```
indexcrop,x_200,i_0  -> 200x300，左半部分   （红色在蓝色上）
indexcrop,x_200,i_1  -> 200x300，右半部分   （绿色在黄色上）
indexcrop,y_150,i_1  -> 400x150，下半部分   （蓝色和黄色）
indexcrop,x_150,i_2  -> 100x300              （短的最后一个切片，保留）
indexcrop,x_200,i_5  -> 400 "i is 5 but the image only has 2 slice(s) of 200 px"
```

80×40 品红色 logo 的 `watermark` 落在锚定角落（`g_se`/`g_nw`/`g_center`
都通过扫描该区域的品红色验证），不透明度混合：在黄色象限上，
`t_20` 给出 `(255,215,40)`，`t_100` 给出 `(255,54,200)`。
`../../etc/passwd` 的水印键、根目录外的符号链接和缺失文件都返回相同的 400
——`watermark image not found`——因此端点不能用于探测文件系统。

`format,auto`：

```
Accept: image/avif,image/webp,*/*   -> image/avif   Vary: Accept
Accept: image/webp,*/*              -> image/webp   Vary: Accept
Accept: */*                         -> image/png    （源格式）
Accept: （缺失）                     -> image/png
```

三种不同的能力产生了三个缓存条目；最后两个共享一个。

对 160×160 的逐像素随机噪声图像的 `pixelate`，计算输出中的不同颜色数
——块数应恰好为 (160/n)²：

```
n        不同颜色数      预期值     16x16 角中的不同数
（无）            25,584     25,600                          256
2                  6,397      6,400                           64
4                  1,595      1,600                           16
8                    399        400                            4
16                    98        100                            1
40                    16         16                            1
```

对 300×200 图像上 (40,30) 处 120×80 红色块的 `trim`——边距为
左 40、右 140、上 30、下 90：

```
（无）                  300x200
trim                    120x80    边框检测并完全移除
trim,t_0                120x80    平坦边框不需要容差
trim,c_FFFFFF           120x80    相同的边框，显式指定
trim,eh_1               220x80    每边 40，较小的边距
trim,ev_1               120x140   上下各 30
trim,eh_1,ev_1          220x140
```

在同一图像的*黑色*边框副本上，`trim` 仍然给出 120×80，
而 `trim,c_FFFFFF` 正确地不做任何事（300×200）——显式颜色路径确实使用给定的颜色。
在边框带有 ±6 噪声的副本上，`trim,t_0` 无法裁剪（300×200），
`trim,t_20` 可以（120×80）。

链接：`trim/resize,w_60/format,avif` 给出 60×40 的 AVIF。

从编码字节读回的 `interlace`——JPEG `SOF` 标记和 PNG `IHDR` 隔行字节：

```
format,jpg                    baseline (SOF0)      830 B
interlace,0/format,jpg        baseline (SOF0)      830 B
interlace,1/format,jpg        PROGRESSIVE (SOF2)  1068 B
format,png                    none                 851 B
interlace,1/format,png        ADAM7               1100 B
interlace,1/format,webp       正常编码，格式中没有此概念
```

对构造已知均值的图像的 `average-hue`：

```
纯色 (120,60,180)                 0x783cb4   精确
一半红色 | 一半蓝色              0x800080   (127.5, 0, 127.5) 四舍五入
灰度 L=200                       0xc8c8c8
一半透明 | 一半绿色              0x00ff00   透明部分不参与投票
25% 透明画布上的红色方块          0xff0000
alpha 128 的均匀蓝色             0x0000ff
完全透明                         0x000000
```

两个查询都是终止操作：`info/format,png`、`resize,w_10/average-hue`、
`average-hue,r_1` 和 `info/average-hue` 都返回 400。

对 300×200 源的 `rotate`。每个角度都落在三角学预测的边界框上，
只有 90 的倍数保持画幅：

```
rotate,0    300x200 RGB     rotate,90   200x300 RGB
rotate,30   360x323 RGBA    rotate,45   354x354 RGBA
rotate,70   291x350 RGBA    rotate,359  303x205 RGBA
```

角落在 PNG 上输出 `a=0`，在 JPEG 上为白色，
`rotate,45/format,auto` 带 AVIF `Accept` 返回 AVIF **RGBA**，
而相同请求不带旋转返回 RGB，
`resize,m_fill,w_100,h_100/rotate,45` 给出 141×141——裁剪先发生，
因此旋转不会被剪掉。

`watermark`，在 600×400 画布上通过墨水落在哪个象限和每层的质心测量：

```
默认 (se, 10px)      墨水仅在 SE       g_nw               墨水仅在 NW
g_center,voffset_-150   墨水移到顶部行
P_50                    1172 -> 30297 墨水像素  （logo 缩放到基础的一半）
fill_1                  四个象限各 29300 像素——均匀平铺
fill_1,padx_100,pady_100 每象限 2724 像素——间距生效
t_20                    相同覆盖，可见地淡
```

文字：`text_` 通过 Pango 渲染——拉丁文和 CJK 都支持——`size_`、`color_`、
`shadow_` 和 `rotate_` 各自按要求改变像素。图像 + 文字：
`order_0` 将 logo 放在 x≈40，文字在 x≈147；`order_1` 交换它们到 x≈182 和 x≈67。
12px 文字旁有 40px logo，`align_0/1/2` 将文字放在第 10–22、23–35 和 37–49 行，
对应 logo 的 15–44——顶部、中间、底部。

十二个格式错误的水印 URL 返回 400 并说明真实原因，
包括 `../../etc/passwd` 的 base64 对象键，被拒绝为
"watermark image not found" 而不是确认存在什么。

对携带完整 EXIF 块和分辨率元数据的 JPEG 的 `info` 返回 34 个字段
——5 个基本字段、3 个分辨率字段，加上 `Make`、`Model`、`Software`、
`DateTime`、`Orientation`、`ExposureTime`、`FNumber`、`ISOSpeedRatings`、
`LensModel`、GPS 块等。相同的 EXIF 写入 PNG（`eXIf`）和 WebP（`EXIF` 块）
返回相同的 34 个字段，因此三个容器路径一致。`UserComment`（`UNDEFINED` 标签）
被跳过。既没有 EXIF 也没有分辨率元数据的图像恰好返回 5 个字段。

对 300×200 纯蓝色源的 `circle` 和 `rounded-corners`，读取 PNG 结果的 alpha 通道：

```
circle,r_50           101x101   中心 a=255，角落 a=0，边缘像素 a=127
circle,r_4096         199x199   限制到 (min edge - 1) / 2 = 99，与 OSS 一致
rounded-corners,r_30  300x200   角落 a=0，边中点 a=127，中心 a=255
rounded-corners,r_4096 300x200  限制到较短边的一半：体育场形，
                                因此上边中点保持 a=255
```

`a=127` 的读数是抗锯齿弧线：恰好一像素宽，在半径处，两种形状都是。
在已经 50% 透明的源上，`circle` 保持内部为 `a=128` 而不是推到 255
——掩码是相乘的。输出格式跟随：`circle,r_40/format,auto` 带
`Accept: image/avif,...` 返回 AVIF RGBA；相同请求不带 `circle` 返回 AVIF RGB。
使用 `format,jpg` 时外部是白色。在 3 帧动画 GIF 上，
`circle,r_20/format,webp` 给出 3 帧 41×41 WebP，每帧都被掩码——
测量时需要 `IXOSS_MAX_ANIMATION_FRAMES=20`，
现在默认保持动画后不再需要。

对 256×64 图像的 `bright` 和 `contrast`，包含完整的 0..255 灰度渐变，
采样 x = 0、64、128、192、255：

```
（无）              [  0,  64, 128, 192, 255]
bright,20           [ 51, 115, 179, 243, 255]   +51 = 20 * 2.55
bright,100          [255, 255, 255, 255, 255]
bright,-100         [  0,   0,   0,   0,   0]
contrast,0          [  0,  64, 128, 192, 255]   与无操作逐位一致
contrast,-100       [127, 127, 127, 127, 127]
contrast,50         [  0,   0, 128, 255, 255]
bright,20/contrast,20 [ 0,  83, 179, 255, 255]
```

16 位灰度源给出相同的数字，因为该步骤在应用枢轴之前转换为 8 位 sRGB。
在 RGBA 源上，`bright,50` 和 `contrast,50` 都保持 alpha 为 128。

`pixelate,1` 返回与完全没有 pixelate 相同的字节（解码像素的相同 SHA-256）。
链接：`crop,w_120,h_80,g_nw/pixelate,20` 给出 120×80，恰好 24 种颜色，
即 6×4 个块。

缓存驱逐，十个条目共 438,603 字节，最大的 88,298：
200,000 字节的限制留下两个条目和 164,834 字节。60,000 字节的限制清空了缓存，
这是正确的——最大的单个条目本身就超过了该预算。

路径安全，全部返回 404 无内容泄露：

```
/../etc/passwd            /..%2f..%2fetc%2fpasswd    /sub/../../etc/passwd
/etc/passwd               /  （目录）
/escape.jpg               （根内的符号链接指向 /etc/passwd）
```

参数错误，全部 400 带 OSS 信封：`image/info,x_1`、`image/format`、
`image/format,tga`、`image/info/format,avif`、`image/resize,w_100`、`video/info`。

`go test ./internal/...` 覆盖语法、路径解析器、头部解析器、缓存驱逐器、
水印提供者的缓存规则、签名和 info 回退。这些包中的大多数是纯逻辑，
不需要 libvips，这是有意为之——参见下面关于 `Validate` 与 `CheckEncoders` 的说明。

`internal/vips`、`internal/processing` 和 `internal/httpserver` 是例外，
必须如此：它们的测试初始化真实的 libvips，在不可用时使测试进程失败
而不是悄悄跳过广泛的覆盖。CI 必须安装 libvips 开发包和 `pkg-config`。
它们检查 TIFF 是否加载、TIFF 守卫依赖的参数查找是否正确回答，
以及编码器探针是否同意在没有任何编码器特殊对待的大小上的真实编码。
上面的像素级检查仍然不在其中：它们需要真实图像，而且是手动运行的。

CI（`.github/workflows/ci.yml`）在 Alpine 3.23（`golang:1.26-alpine3.23`）上
构建并运行这些测试，安装 `vips-heif` 和 `vips-jxl`，
针对相同的 libvips 头运行 `staticcheck`，并添加 `gofmt` 和 `go mod tidy` 检查。
`.github/workflows/govulncheck.yml` 在每次更改和每周检查可达的已知漏洞，
因为新公告可能在没有推送的情况下到达。cgo 项目需要构建本身就是测试
——"在这里能编译"是关于一个 libvips 的声明，不是关于 Go 的。

## 尚未构建

OSS 自己列表中的每个操作都已实现。剩下的不是操作。

**不是操作**

- `padding`、`extend` 和焦点重力是 imgproxy 的功能，没有 OSS 语法。
  `extend` 已经可以通过 `resize,m_pad` 实现；其他两个需要 iXoSS 自创的操作名。

**版本依赖**

- **动画 JPEG XL 需要 libvips 8.16。** `jxlload` 在 8.16 中获得了动画
  ——以及 `page`/`n` 加载选项。`vips_jxlload_source_go`
  仅在针对 8.16 或更新版本构建时传递它们；在较旧的 libvips 上，
  它将 JXL 加载为单帧，这是自洽的，因为该加载器也不设置页面元数据，
  因此 `IsAnimated()` 保持 false，动画路径永远不会被采用。
  静态图像和 JXL 输出在任何情况下都不受影响。
- **动画 PNG 不能被任何已发布的 libvips 读取。** `pngload` 和 `pngsave`
  在 master 上获得了 APNG，在 `date-tbd 8.19.0` 标题下；最新的标签是 v8.18.6，
  因此没有可以升级到的版本。当有时，仅它本身也不够：
  代码在 libpng 路径（`vipspng.c`）中的 `#ifdef PNG_APNG_SUPPORTED` 后面，
  因此还需要支持 APNG 的 libpng——1.8+，或带 APNG 补丁的 1.6
  ——而 libspng 路径完全没有。在构建能实际读取这些帧之前，
  APNG 对管线来说是静态的，请求它作为 WebP 或 GIF 会被拒绝而不是扁平化；
  参见动画下的 APNG 说明。当那天到来时不需要任何更改：
  拒绝比较容器的帧数与 libvips 加载的，而不是版本号，
  因此它在恰好能处理的构建上自动停止。
- **TIFF 的 `unlimited` 加载标志需要 libvips 8.17 配合 libtiff 4.7+ 构建。**
  iXoSS 询问加载器是否有该属性，而不是读取版本，
  因为 8.17 在较旧的 libtiff 上不注册它。不存在时，libvips 保持自己的解码限制，
  TIFF 正常加载。

## 两个值得保留的设计说明

`ossprocess.Validate()` 仅检查语法。libvips 是否能实际*编码*某种格式是
`CheckEncoders()`，在 `vips.Init()` 之后单独调用。

它们分开是因为 `vips.SupportsSave()` 调用 C 库，
如果 `vips_init` 没有运行，libvips 会用 `SIGABRT` 终止进程——
不是错误返回，是崩溃。触及它的语法检查无法进行单元测试，
任何在启动前到达它的代码路径都会使进程崩溃。
测试套件在第一次运行时就发现了这一点。

**`resize` 相对于源解析，而不是在解析器中。** OSS 的三个参数
在不知道源尺寸的情况下无法变成缩放因子：`m_mfit` 和 `s_` 约束*较小*的
输出边，这取决于宽高比，而 `limit_1` 是与源的比较。
因此 `ossprocess.Resize` 是描述，`Resolve(srcW, srcH)` 产生具体的目标。
`Chain.NeedsSourceSize()` 让服务器为不需要它的链跳过头部读取
——仅 `format,avif`（常见情况）仍然零成本。

这也解释了 `m_mfit` 映射到 `ResizeForce` 而不是 `ResizeFill`：
imgproxy 的 Fill 缩放到覆盖*然后裁剪*，那是 `m_fill`。对于 `m_mfit`，
覆盖尺寸在这里计算并强制，因此不裁剪任何内容。

## 关于 JPEG XL 的说明

`jxlsave` 可以工作并已连接，但浏览器支持率全球为 14.63%（caniuse）：
Chrome 145–155 在标志后面有它，**Edge 不支持**，Firefox 不支持，
Safari 17+ 是部分的。仅通过 `Accept` 协商提供，预期流量接近零。

## 许可证

iXoSS 采用 **GNU Affero General Public License，第 3 版**（`LICENSE`）许可。
它是一个网络服务，因此第 13 条是关键条款：修改它并让其他人通过网络使用，
你就有义务向他们提供修改后的源代码。

图像引擎是 imgproxy 的，采用 Apache-2.0 许可证（`LICENSE-imgproxy`）。
Apache-2.0 在第 3 版与 GPL 家族单向兼容，因此基于它构建的作品可以在
AGPL-3.0 下分发——兼容性不是双向的，这里也没有重新许可 imgproxy 本身。
libvips 是 LGPL-2.1 *或更新版本*，正是"或更新版本"使链接合法：
它允许 LGPL-3.0，与 AGPL-3.0 兼容。`NOTICE` 有完整的署名、
逐文件来源和第三方列表。

修改过的 imgproxy 文件带有单独的修改声明。原始上游 NOTICE 和额外的
Go/模块/字体许可证文件保留在 `third_party/` 下，并包含在 Docker 镜像中。

运行时 Docker 镜像不包含源代码归档。导出每个版本的实际构建源代码、
构建说明和供应商 Go 依赖：

```sh
docker buildx build --target source-export \
  --output type=local,dest=./dist .
```

将 `dist/ixoss-source.tar.gz` 作为相应标签版本的资产上传，
并为镜像接收者提供清晰的下载说明。从相同的未更改源代码树和构建器镜像
构建运行时镜像，使用
`--build-arg IXOSS_SOURCE_URL=https://github.com/vndroid/iXoSS/releases/download/<tag>/ixoss-source.tar.gz`，
或在部署时设置 `IXOSS_SOURCE_URL`。该 URL 必须免费提供该构建的完整对应源代码，
并按许可证要求保持可用。变动的分支链接或 GitHub 的自动标签归档 alone
不能替代此构建输入归档，后者还包含供应商代码。
在分发镜像或依赖链接进行所需的网络源代码提供之前发布源代码。
没有配置的归档或 URL，程序不会宣传源代码提供。

或者，挂载匹配的源代码归档并将 `IXOSS_SOURCE_ARCHIVE` 配置为其可读路径。
然后 HTTP 响应宣传 GET `/_ixoss/source.tar.gz`（HEAD 和范围请求也可以在
没有图像签名的情况下工作）。当 `IXOSS_SOURCE_ARCHIVE` 被配置时，此路径被保留。
它只读取配置的受信任归档，不读取图像目录或私有工作区。

独立部署可以将 `IXOSS_SOURCE_ARCHIVE` 设置为可读的源代码归档，
或将 `IXOSS_SOURCE_URL` 设置为该构建的绝对 HTTP(S) 源代码下载 URL。
归档优先；两者默认未设置，除非在镜像构建时提供了 URL。
通过代理保持源代码提供可访问，并在更改运行代码时更新它。
参见[分发声明](third_party/licenses/README.md)了解包清单、原始声明和原生库源代码义务。
