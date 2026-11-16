<!-- SPDX-License-Identifier: MPL-2.0 -->

# xml

Namespace-aware XML 1.0 reading and writing with bounded memory. Encoding, deterministic.

Three views share one parser: `decode` for small documents, record selection for
large documents made of repeated elements, and a cursor for everything else.
The writer mirrors the reader, so read-transform-write pipelines run in constant memory.

## Loading

```lua
local xml = require("xml")
```

## Element

Every tree the module produces or accepts is built from one plain table type.
Elements carry no metatable and survive messages, payloads and `json.encode`.

```lua
type Element = {
    name: string,                    -- local name
    ns: string?,                     -- namespace URI; absent = no namespace
    attrs: {[string]: string}?,      -- attribute values by key
    xmlns: {[string]: string}?,      -- namespace declarations: prefix -> URI, "" = default
    text: string?,                   -- content, when it is text only
    children: {string | Element}?,   -- content, when it holds at least one element
}
```

Normal form, guaranteed on output and required on input:

- An element has `text`, or `children`, or neither. Never both.
- `children` holds strings and Elements in document order, as a dense array.
  Adjacent text is one string. Empty strings do not appear.
- `<a/>` and `<a></a>` are the same element: neither `text` nor `children`.
- Empty `attrs`, `xmlns` and `children` tables are omitted.
- All values are strings. Numbers and booleans are never inferred.

On input, a table that breaks these rules is rejected with code `element`: an empty
`text` string, empty `attrs`, `xmlns` or `children` tables, a sparse or non-array
`children`, and non-string names or values. Fields other than the six above are ignored.

Names and namespaces:

- `name` is the local name; `ns` is the namespace URI the element resolves to.
- An unqualified attribute is keyed by its local name: `attrs.id`.
  A qualified attribute is keyed `{uri}local`: `attrs["{http://www.w3.org/1999/xlink}href"]`.
  The local name follows the last `}` of the key.
- Unprefixed attributes have no namespace; the default namespace applies to elements only.
- `xmlns` holds the declarations written on that element. Declarations are not attributes
  and never appear in `attrs`. `xmlns[""] = ""` undeclares the default namespace.
- The root of every materialised tree (`decode`, `reader:node`, `records:read`) carries
  the complete set of bindings in scope at that element, so a detached record resolves
  QName-valued content such as `xsi:type="s:Order"` without its ancestors.
  Descendants carry only their own declarations.
- The `xml` prefix is always bound to `http://www.w3.org/XML/1998/namespace` and is never listed.

Content rules:

- Character references and the five predefined entities are expanded. CDATA sections
  are text. Round trips preserve meaning, not lexical form.
- Comments and processing instructions never enter a tree.
- Whitespace-only text is preserved unless `whitespace = "strip"` is set.

## Options

Reader and decode options:

| Option | Type | Default | Notes |
|--------|------|---------|-------|
| whitespace | string | `"preserve"` | `"strip"` drops whitespace-only character data, see Whitespace |
| comments | boolean | false | Cursor reports `"comment"` events |
| instructions | boolean | false | Cursor reports `"pi"` events |
| max_depth | integer | 256 | Open elements |
| max_attrs | integer | 256 | Attributes plus namespace declarations on one element |
| max_namespaces | integer | 1024 | Namespace declarations in scope at once, shadowed ones included |
| max_tag_bytes | integer | 1048576 | One start tag with all attribute values, or one reported comment or processing instruction |
| max_token_bytes | integer | 1048576 | Largest text chunk a cursor event carries |
| max_node_bytes | integer | 8388608 | Source bytes spanned by one materialised element |
| max_bytes | integer | 0 | Whole document; 0 = unlimited |

Limit options of the reader and the writer must be integers of at least 1; `max_bytes` must be at least 0, where 0 means
unlimited. Limits count bytes of the document in UTF-8 form, so both runtimes fail on the same input.
`decode` is bounded by `max_node_bytes`: the root element is the materialised node.

Writer and encode options:

| Option | Type | Default | Notes |
|--------|------|---------|-------|
| indent | string | none | Spaces or tabs added per level, at most 256 bytes, see Indentation |
| declaration | boolean | false | Emit `<?xml version="1.0" encoding="UTF-8"?>` |
| ns | {[string]: string} | none | Preferred prefix -> URI bindings, declared on the root |
| max_depth | integer | 256 | Open elements |
| max_attrs | integer | 256 | Attributes plus namespace declarations written on one element, generated ones included |
| max_namespaces | integer | 1024 | Namespace declarations in scope at once, shadowed ones included |
| max_node_bytes | integer | 8388608 | Serialised bytes of one tree written by `encode`, `writer:node` or `writer:element` |
| max_tag_bytes | integer | 1048576 | Serialised bytes of one start tag with its declarations and attributes, or one comment |

The writer checks its limits before it appends each part of the output (start tag,
declaration, attribute, text, comment, processing instruction, closing bracket, end tag,
indentation), using the bytes that part writes after escaping, so a table that shares subtrees or holds large values cannot expand past them: the
work stops at the limit with a `limit` error naming the option. `max_tag_bytes` applies to
every write, including `writer:comment` and `reader:copy`.
`max_depth`, `max_attrs` and `max_namespaces` apply to every write, including
`reader:copy`. `max_node_bytes` does not apply to `reader:copy` or to elements opened with
`writer:start_element`: those stream in bounded memory, and a copied element is already
bounded by the reader's limits.

## Whitespace

With `whitespace = "strip"` the parser drops a run of character data when all of the
following hold:

- the run lies between two pieces of markup and consists only of literal space, tab,
  carriage return and line feed characters;
- it contains no character or entity reference and is not a CDATA section;
- it is no longer than `max_token_bytes`;
- no enclosing element sets `xml:space="preserve"` without a nearer `xml:space="default"`.

The decision covers the whole run: whitespace next to other text is content and is
never removed. Stripped runs produce no cursor event.

## Indentation

With `indent` set, the serialiser starts each element on its own line.

- `encode` and `writer:node` see the whole tree: an element is indented inside only
  when all of its children are elements.
- `writer:start_element`, `writer:element` and `reader:copy` write incrementally: a
  line break precedes a child element or a closing tag while the parent has received
  no text. Once text is written into an element, nothing is added inside it.

Indentation is meant for data documents. Mixed content written incrementally keeps
its text but may gain a line break before a leading child element.

An element without content is written as `<name/>`. The XML declaration is followed by
a line break when `indent` is set.

## Functions

### decode(data: string, options?: table) → Element, error

Parses one complete document and returns its root element. Reads to the end of input:
content after the root element other than whitespace, comments and processing
instructions is an error.

### encode(element: Element, options?: table) → string, error

Serialises an element as UTF-8. Output is deterministic: namespace declarations come
first in prefix order, then attributes ordered by namespace URI and local name.

Prefix selection for an element or attribute in namespace `U`:

1. A binding for `U` already in scope (the default namespace first for elements, then the
   smallest prefix).
2. Otherwise a new declaration on that element: the default namespace for an element,
   a generated prefix `ns1`, `ns2`, ... for a qualified attribute.

An element without a namespace inside a default-namespace scope emits `xmlns=""`.

### text(element: Element, options?: table) → string

Returns the concatenated text of the element and all its descendants, in document order.

`text` and `find` walk descendants within a budget: `max_node_bytes` (default 8388608), the
source a tree decoded under that limit could hold, where each element visited costs 4 and
each text byte 1, and `max_depth` (default 256). Tables that share subtrees or form a cycle
therefore end in a `limit` error naming the option. `text` raises that error, like a wrong
argument type, so its result stays a string. `attr`, `child`, `children` and `resolve` read
only the element and its direct children and take no budget.

### attr(element: Element, name: string, ns?: string) → string?

Returns an attribute value by local name and optional namespace URI.

### child(element: Element, name: string, ns?: string) → Element?

Returns the first child element with that local name. With `ns`, the namespace must
match; `ns = ""` matches elements without a namespace. Without `ns`, any namespace matches.

### children(element: Element, name?: string, ns?: string) → {Element}

Returns the child elements, filtered like `child`. Text children are skipped.

### find(element: Element, path: string, options?: {ns: table, max_node_bytes: integer, max_depth: integer}) → {Element}?, error

Returns the descendants matching a path, relative to `element`. `options.ns` binds the path
prefixes. A malformed path, a prefix missing from `ns` or an exhausted budget returns `nil`
and an error.

### resolve(element: Element, qname: string) → string, string?, error

Resolves a QName such as `s:Order` against the `xmlns` bindings of `element` and returns
the local name and namespace URI. Use the root of a materialised tree, which carries
the full scope.

### reader(source: string | Stream | File, options?: table) → Reader, error

Creates a reader over a string or a readable stream. Any readable stream object the
runtime hands out works, such as a `stream.Stream` or an `fs.File` opened for reading. The reader reads ahead: until it
is closed, the stream position is unspecified and the stream must not be read by
anything else. Closing the reader leaves the stream open.

### writer(dest?: Stream | File, options?: table) → Writer, error

Creates a writer over a writable stream. Any writable stream object the runtime hands
out works, such as a `stream.Stream` or an `fs.File` opened for writing. Without `dest` the writer accumulates in
memory and `writer:finish()` returns the document as a string.

## Paths

Paths select elements by expanded name. They are used by `find` and `reader:select`.

```
path  = ["/"] step {"/" step}
step  = "*" | name | prefix ":" name | "*:" name | prefix ":*"
```

- `name` matches an element with that local name and no namespace.
- `prefix:name` matches the namespace bound to `prefix` in the `ns` option. Prefixes in
  paths are independent of the prefixes the document uses. An unbound prefix is an error.
- `*:name` matches the local name in any namespace. `*` matches any element.
- In `reader:select`, a path is anchored at the document root: the first step matches
  the root element. In `find`, the first step matches children of the given element.

## Types

### Reader

A forward-only cursor. Methods that may wait for input are marked *yields*.

#### reader:select(paths: string | {string}, options?: {ns: table}) → Records, error

Starts record selection. Matching elements are delivered whole, in document order.
Matches do not nest: an element inside a delivered record is not delivered again.
Selection takes over the reader; cursor methods return an error afterwards.

#### reader:next() → string?, error — yields

Advances to the next event and returns its kind: `"start"`, `"end"`, `"text"`,
`"comment"` or `"pi"`. Returns `nil, nil` at the end of the document. Text longer than
`max_token_bytes` arrives as consecutive `"text"` events split on character boundaries.

#### reader:kind() → string?

The current event kind.

#### reader:name() → string?

Local name on `"start"` and `"end"`; target on `"pi"`.

#### reader:ns() → string?

Namespace URI on `"start"` and `"end"`.

#### reader:attr(name: string, ns?: string) → string?

One attribute of the current `"start"` event.

#### reader:attrs() → {[string]: string}

All attributes of the current `"start"` event, keyed as in `Element.attrs`.

#### reader:text() → string?

Content on `"text"`, `"comment"` and `"pi"`.

#### reader:depth() → integer

Number of open elements, counting the current `"start"`.

#### reader:pos() → {line: integer, column: integer, offset: integer}

Position of the current event. `line` and `column` are one-based, `column` counts
characters. `offset` is the zero-based byte offset into the document in UTF-8 form.

#### reader:node() → Element, error — yields

On `"start"`: materialises the current element and leaves the cursor on its `"end"`.

#### reader:skip() → boolean, error — yields

On `"start"`: discards the current element and leaves the cursor on its `"end"`.
Nothing is materialised.

#### reader:copy(writer: Writer) → boolean, error — yields

On `"start"`: writes the current element to `writer` without materialising it and
leaves the cursor on its `"end"`. Namespaces the element inherits are declared in the output.

#### reader:close() → boolean

Releases the reader. Idempotent. Stopping before the end does not validate the
remainder of the document.

### Records

#### records:read(n?: integer) → {Element}?, error — yields

Returns up to `n` records (default 64). A batch also ends once its records span
`max_node_bytes` of source. Returns `nil, nil` when the document ends. An error is
sticky: every later call returns it.

#### records:close() → boolean

Releases the selection and its reader.

### Writer

#### writer:start_element(name: string, attrs?: table, ns?: string) → boolean, error

Opens an element. `attrs` is keyed as in `Element.attrs`.

#### writer:end_element() → boolean, error

Closes the innermost open element.

#### writer:text(data: string) → boolean, error

Writes escaped character data inside the open element.

#### writer:element(name: string, attrs?: table, text?: string, ns?: string) → boolean, error

Writes a complete element with optional text content.

#### writer:node(element: Element) → boolean, error

Writes a complete tree.

#### writer:comment(data: string) → boolean, error

Writes a comment.

#### writer:flush() → boolean, error — yields

Writes buffered output to the stream. Other writer methods yield when the buffer fills.
On an in-memory writer it succeeds without yielding.

#### writer:finish() → string?, error — yields

Completes the document and flushes. Fails if elements are still open or no root
element was written. Returns the document for an in-memory writer, `nil` otherwise.
Calling it again is a `state` error.

#### writer:close() → boolean

Releases the writer. Idempotent. Leaves the stream open and never completes an
unfinished document.

A failed write is terminal: output already sent is never replayed and every later call
returns the same error.

## Well-formedness

The parser accepts XML 1.0 (Fifth Edition) with Namespaces in XML 1.0, and nothing else:

- One root element. `version="1.0"` only.
- A document type declaration is an error. External resources are never fetched.
- Entity references other than `&lt; &gt; &amp; &apos; &quot;` and character references are errors.
- Duplicate attributes are errors, including two prefixes bound to the same namespace.
- Unbound prefixes and misuse of the `xml` and `xmlns` prefixes are errors.
- Characters outside the XML `Char` range are errors, literal or as references.
- Line endings are normalised to `\n`. Attribute values have tabs and line breaks normalised to spaces.

Encodings: UTF-8, UTF-16 (byte order mark or declaration), ISO-8859-1, windows-1252
and US-ASCII. A byte order mark takes precedence; a declaration that contradicts it,
an unsupported encoding and malformed input are errors. Malformed bytes are never replaced.

## Errors

| Condition | Kind | details.code |
|-----------|------|--------------|
| wrong argument type or option | errors.INVALID | `argument` |
| malformed document | errors.INVALID | `syntax` |
| namespace error | errors.INVALID | `namespace` |
| unsupported or malformed encoding | errors.INVALID | `encoding` |
| limit exceeded | errors.INVALID | `limit` |
| element table outside normal form | errors.INVALID | `element` |
| method not valid in the current state | errors.INVALID | `state` |
| stream read or write failed | errors.INTERNAL | `io` |
| operation canceled | errors.CANCELED | `canceled` |

A method of a reader, records or writer reports, in this order: the failure that made the
handle terminal, which every later method returns unchanged; that the handle is closed;
for a reader, that a record selection owns it; then problems with its own arguments and
state. A failed `next`, `node`, `skip`, `copy` or `records:read` and every writer failure are
terminal; a `copy` that fails on its first event or later, on the reader's side or the
writer's, makes the reader terminal with that error. A `copy` into a writer that is already
closed or failed reports the writer's error without touching the reader. Calling a cursor
method on the wrong event is a `state` error and is not terminal.

The accessors `kind`, `name`, `ns`, `attr`, `attrs`, `text`, `depth` and `pos` have no error
result. On a reader that has failed, is closed or is owned by a record selection they
return `nil`, an empty table from `attrs`, `0` from `depth` and a zero position from `pos`,
before looking at their arguments.

Parse errors carry `line`, `column`, `offset` and `path` (the open element names) in
`err:details()`. Limit errors, from the reader or the writer, also carry `limit`, the option name.

## Example

```lua
local xml = require("xml")

-- small document
local doc, err = xml.decode('<order id="7"><total>12.50</total></order>')
if err then return nil, err end
local total = xml.child(doc, "total")

-- large feed, one record at a time
local function each_item(feed, process)
    local reader, err = xml.reader(feed, {whitespace = "strip"})
    if err then return nil, err end
    local records, serr = reader:select("s:catalog/s:item", {ns = {s = "urn:shop"}})
    if serr then return nil, serr end
    while true do
        local batch, rerr = records:read(100)
        if rerr then
            records:close()
            return nil, rerr
        end
        if not batch then break end
        for _, item in ipairs(batch) do
            process(item.attrs and item.attrs.id, xml.text(item))
        end
    end
    records:close()
    return true
end

-- constant-memory transform
local function active_items(input, out)
    local reader, err = xml.reader(input)
    if err then return nil, err end
    local w, werr = xml.writer(out, {indent = "  "})
    if werr then return nil, werr end
    w:start_element("items")
    while true do
        local kind, nerr = reader:next()
        if nerr then return nil, nerr end
        if not kind then break end
        if kind == "start" and reader:name() == "item" then
            if reader:attr("status") == "active" then
                reader:copy(w)
            else
                reader:skip()
            end
        end
    end
    w:end_element()
    local document, ferr = w:finish()
    reader:close()
    w:close()
    if ferr then return nil, ferr end
    return document or true
end
```
