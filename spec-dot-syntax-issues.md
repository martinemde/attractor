# DOT Syntax Issues in attractor-spec.md

Issues found by reviewing the examples against the BNF grammar (Section 2.2)
and constraints (Section 2.3).

## 1. `Value` production doesn't include bare identifiers (pervasive)

The grammar (line 99) defines:

```
Value ::= String | Integer | Float | Boolean | Duration
```

But nearly every example uses unquoted identifiers as attribute values — most
commonly for `shape`:

- `shape=Mdiamond` (lines 259, 277, 1508, 1928)
- `shape=Msquare` (lines 260, 278, 1509, 1932)
- `shape=box` (lines 234, 275, 1929-1931)
- `shape=diamond` (line 282)
- `shape=hexagon` (line 301)

`Mdiamond`, `box`, `diamond`, etc. are `Identifier` tokens, but `Identifier`
is not an alternative in the `Value` production. These should either be quoted
(`shape="Mdiamond"`) or the grammar should add `Identifier` to `Value`.

**Fix (grammar):** Change `Value` to:

```
Value ::= String | Integer | Float | Boolean | Duration | Identifier
```

**Fix (examples):** Quote all bare identifier values: `shape="Mdiamond"`, etc.

## 2. `Direction` production is orphaned

Line 107 defines:

```
Direction ::= 'TB' | 'LR' | 'BT' | 'RL'
```

But `Direction` is never referenced by any other production. It's not included
in `Value`. Multiple examples use `rankdir=LR` (lines 257, 274, 295), which is
invalid because `LR` doesn't match `String`, `Integer`, `Float`, `Boolean`, or
`Duration`.

**Fix:** If `Identifier` is added to `Value` (issue #1), this is resolved since
`LR` matches `Identifier`. Otherwise, add `| Direction` to `Value`. The
standalone `Direction` production could then be removed or kept as documentation.

## 3. Section 11.10 contradicts the stylesheet grammar (Section 8.2)

The stylesheet grammar defines three selectors:

```
Selector ::= '*' | '#' Identifier | '.' ClassName
```

But Section 11.10 (line 1875) describes a shape selector that doesn't exist in
this grammar:

> `box { model = "claude-opus-4-6" }`

Three problems in this one example:

- **`box` as a selector** — there's no bare-identifier/shape selector type.
  Only `*`, `#id`, and `.class` are defined.
- **`model` as a property** — Section 8.4 defines the property as `llm_model`,
  not `model`.
- **`=` as separator** — The `Declaration` grammar (line 1461) uses `:`
  (`Property ':' PropertyValue`), not `=`.

The specificity order on line 1878 also references "shape"
(`universal < shape < class < ID`), but shape selectors aren't defined in
Section 8.3's specificity table, which only has three levels (universal=0,
class=1, ID=2).

**Fix:** Either:

a) Remove shape selectors from Section 11.10 and the specificity order, aligning
   with the grammar. Change the example to use a valid selector like
   `* { llm_model: "claude-opus-4-6"; }`.

b) Add shape selectors to the grammar and specificity table:
   ```
   Selector ::= '*' | Identifier | '#' Identifier | '.' ClassName
   ```
   And update the specificity table to 4 levels. Fix `model` -> `llm_model` and
   `=` -> `:` in the example regardless.

## 4. Implicit node declarations in Human gate example

In the Human gate example (lines 306-309), `ship_it` and `fixes` appear only in
edge statements and are never explicitly declared with a `NodeStmt`. The other
two examples in Section 2.13 explicitly declare every node before referencing
them in edges.

This isn't strictly a grammar violation, but the spec doesn't document whether
implicit node creation from edge references is supported.

**Fix:** Either add explicit node declarations to the example:

```dot
ship_it [label="Ship It"]
fixes   [label="Fixes"]
```

Or document that nodes referenced in edges are implicitly created (matching
standard DOT behavior).

## 5. `rankdir` is undocumented

`rankdir` is used in three examples (lines 257, 274, 295) but is not listed in
the graph-level attributes table (Section 2.5). It's a standard Graphviz
attribute for rendering.

**Fix:** Either add `rankdir` (type: Direction, default: `TB`) to Section 2.5's
table, or add a note that standard Graphviz layout attributes are passed through
for rendering but ignored by the execution engine.
