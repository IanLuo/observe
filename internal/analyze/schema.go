package analyze

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"observe/internal/model"
)

// Schema is the table shape that SQL migrations declare once applied in file-name
// order. It is read statically: no database is connected and no statement runs
// (AC18). SQL is schema input, not a source language, so it never yields
// declarations or links.
type Schema struct {
	Tables      []SchemaTable
	Diagnostics []model.Diagnostic
}

// SchemaTable is one table as the migrations leave it.
type SchemaTable struct {
	Name       string
	Migration  string // file that created the table
	Columns    []SchemaColumn
	References []SchemaReference
	// Unknown is set when a later statement changed the table in a way this reader
	// does not model. The table still shows; the reason says what is missing.
	Unknown string
}

// SchemaColumn is one column. Key marks a primary key column.
type SchemaColumn struct {
	Name string
	Type string
	Key  bool
}

// SchemaReference is one foreign key. From and To read "table.column" or
// "table.column,column" for composite keys.
type SchemaReference struct {
	Constraint string // empty when the migration did not name it
	From       string
	To         string
	Migration  string // file that declared the relation
}

// ReadSchema applies the SQL files in path order and returns the tables that
// remain. A statement that is not table-shaped (indexes, triggers, functions,
// data) is ignored: it does not change the tables. A table statement the reader
// cannot parse is a diagnostic, never a silent gap.
func ReadSchema(files []File) Schema {
	sorted := append([]File(nil), files...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	b := &schemaBuilder{tables: map[string]*SchemaTable{}}
	for _, f := range sorted {
		for _, stmt := range sqlStatements(string(f.Body)) {
			b.statement(f.Path, normalizeSpace(stmt))
		}
	}

	out := Schema{Diagnostics: b.diags}
	for _, name := range b.order {
		if t, ok := b.tables[name]; ok {
			out.Tables = append(out.Tables, *t)
		}
	}
	return out
}

type schemaBuilder struct {
	order  []string
	tables map[string]*SchemaTable
	diags  []model.Diagnostic
}

var (
	createTablePrefix = regexp.MustCompile(`(?i)^CREATE\s+(?:(?:GLOBAL\s+|LOCAL\s+)?(?:TEMP|TEMPORARY)\s+|UNLOGGED\s+)?TABLE\b`)
	createTableRE     = regexp.MustCompile(`(?i)^CREATE\s+(?:(?:GLOBAL\s+|LOCAL\s+)?(?:TEMP|TEMPORARY)\s+|UNLOGGED\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([^\s(]+)\s*\((.*)\)$`)
	alterTablePrefix  = regexp.MustCompile(`(?i)^ALTER\s+TABLE\b`)
	alterTableRE      = regexp.MustCompile(`(?i)^ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?(\S+)\s+(.+)$`)
	dropTableRE       = regexp.MustCompile(`(?i)^DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?(\S+?)\s*(?:CASCADE|RESTRICT)?$`)
	tableConstraintRE = regexp.MustCompile(`(?i)^(?:CONSTRAINT\s+(\S+)\s+)?(PRIMARY\s+KEY|FOREIGN\s+KEY|UNIQUE|CHECK|EXCLUDE)\b`)
	columnDefRE       = regexp.MustCompile(`^("[^"]+"|\S+)\s*(.*)$`)
	typeStopRE        = regexp.MustCompile(`(?i)\s(NOT|NULL|DEFAULT|PRIMARY|REFERENCES|UNIQUE|CHECK|CONSTRAINT|GENERATED|COLLATE)\b`)
	inlineRefRE       = regexp.MustCompile(`(?i)(?:CONSTRAINT\s+(\S+)\s+)?REFERENCES\s+([^\s(]+)\s*(?:\(([^)]*)\))?`)
	inlinePrimaryRE   = regexp.MustCompile(`(?i)\bPRIMARY\s+KEY\b`)
	foreignKeyRE      = regexp.MustCompile(`(?i)FOREIGN\s+KEY\s*\(([^)]*)\)\s*REFERENCES\s+([^\s(]+)\s*(?:\(([^)]*)\))?`)
	primaryKeyColsRE  = regexp.MustCompile(`(?i)PRIMARY\s+KEY\s*\(([^)]*)\)`)
	addColumnRE       = regexp.MustCompile(`(?i)^ADD\s+(?:COLUMN\s+)?(?:IF\s+NOT\s+EXISTS\s+)?(.+)$`)
	addConstraintRE   = regexp.MustCompile(`(?i)^ADD\s+(?:CONSTRAINT\s+(\S+)\s+)?(.+)$`)
	dropConstraintRE  = regexp.MustCompile(`(?i)^DROP\s+CONSTRAINT\s+(?:IF\s+EXISTS\s+)?(\S+)`)
)

func (b *schemaBuilder) diag(path, msg string) {
	b.diags = append(b.diags, model.Diagnostic{Op: "schema", Path: path, Message: msg})
}

func (b *schemaBuilder) statement(path, s string) {
	switch {
	case createTablePrefix.MatchString(s):
		b.createTable(path, s)
	case alterTablePrefix.MatchString(s):
		b.alterTable(path, s)
	case dropTableRE.MatchString(s):
		b.dropTable(s)
	}
}

func (b *schemaBuilder) createTable(path, s string) {
	m := createTableRE.FindStringSubmatch(s)
	if m == nil {
		b.diag(path, "CREATE TABLE is not in a form the reader models: "+clip(s))
		return
	}
	name := identName(m[1])
	items, err := splitTop(m[2])
	if err != nil {
		b.diag(path, "CREATE TABLE "+name+": "+err.Error())
		return
	}
	if _, exists := b.tables[name]; exists {
		b.diag(path, "CREATE TABLE "+name+" replaces a table an earlier migration created")
	} else {
		b.order = append(b.order, name)
	}
	t := &SchemaTable{Name: name, Migration: path}
	b.tables[name] = t
	for _, item := range items {
		b.tableItem(t, path, item)
	}
}

// tableItem reads one comma-separated item of a CREATE TABLE body: a column, or a
// table constraint. Constraints the reader does not model (CHECK, UNIQUE, EXCLUDE)
// are not relations and are ignored.
func (b *schemaBuilder) tableItem(t *SchemaTable, path, item string) {
	if m := tableConstraintRE.FindStringSubmatch(item); m != nil {
		kind := strings.ToUpper(normalizeSpace(m[2]))
		switch kind {
		case "FOREIGN KEY":
			b.addForeignKey(t, path, m[1], item)
		case "PRIMARY KEY":
			if pk := primaryKeyColsRE.FindStringSubmatch(item); pk != nil {
				for _, c := range splitList(pk[1]) {
					markKey(t, identName(c))
				}
			}
		}
		return
	}
	b.column(t, path, item)
}

func (b *schemaBuilder) column(t *SchemaTable, path, def string) {
	m := columnDefRE.FindStringSubmatch(def)
	if m == nil {
		b.diag(path, "column definition not modelled in "+t.Name+": "+clip(def))
		return
	}
	col := SchemaColumn{Name: identName(m[1]), Type: strings.TrimSpace(m[2])}
	if stop := typeStopRE.FindStringIndex(col.Type); stop != nil {
		col.Type = strings.TrimSpace(col.Type[:stop[0]])
	}
	col.Key = inlinePrimaryRE.MatchString(def)
	t.Columns = append(t.Columns, col)

	if r := inlineRefRE.FindStringSubmatch(def); r != nil {
		to := identName(r[2])
		cols := ""
		if r[3] != "" {
			cols = joinNames(r[3])
		}
		t.References = append(t.References, SchemaReference{
			Constraint: identName(r[1]),
			From:       t.Name + "." + col.Name,
			To:         joinTarget(to, cols),
			Migration:  path,
		})
	}
}

func (b *schemaBuilder) addForeignKey(t *SchemaTable, path, constraint, text string) {
	m := foreignKeyRE.FindStringSubmatch(text)
	if m == nil {
		b.diag(path, "foreign key not modelled in "+t.Name+": "+clip(text))
		return
	}
	t.References = append(t.References, SchemaReference{
		Constraint: identName(constraint),
		From:       joinTarget(t.Name, joinNames(m[1])),
		To:         joinTarget(identName(m[2]), joinNames(m[3])),
		Migration:  path,
	})
}

func (b *schemaBuilder) alterTable(path, s string) {
	m := alterTableRE.FindStringSubmatch(s)
	if m == nil {
		b.diag(path, "ALTER TABLE is not in a form the reader models: "+clip(s))
		return
	}
	name, action := identName(m[1]), m[2]
	t, ok := b.tables[name]
	if !ok {
		b.diag(path, "ALTER TABLE "+name+": no earlier migration in the analysed files created this table")
		return
	}

	switch {
	case regexp.MustCompile(`(?i)^ADD\s+CONSTRAINT\b|^ADD\s+FOREIGN\s+KEY\b`).MatchString(action):
		c := addConstraintRE.FindStringSubmatch(action)
		if foreignKeyRE.MatchString(c[2]) {
			b.addForeignKey(t, path, c[1], c[2])
		}
		// CHECK, UNIQUE, and primary keys added later are not relations: ignored.
	case addColumnRE.MatchString(action):
		b.column(t, path, addColumnRE.FindStringSubmatch(action)[1])
	case dropConstraintRE.MatchString(action):
		dropped := identName(dropConstraintRE.FindStringSubmatch(action)[1])
		kept := t.References[:0]
		for _, r := range t.References {
			if r.Constraint != dropped {
				kept = append(kept, r)
			}
		}
		t.References = kept
	default:
		t.Unknown = fmt.Sprintf("%s: %s is not modelled", path, actionName(action))
	}
}

func (b *schemaBuilder) dropTable(s string) {
	m := dropTableRE.FindStringSubmatch(s)
	if m == nil {
		return
	}
	name := identName(m[1])
	if _, ok := b.tables[name]; !ok {
		return
	}
	delete(b.tables, name)
	for i, n := range b.order {
		if n == name {
			b.order = append(b.order[:i], b.order[i+1:]...)
			break
		}
	}
}

// actionName names the statement for an unmodelled ALTER: its verb, plus the object
// when the object is what makes it a different statement ("ALTER COLUMN").
func actionName(action string) string {
	words := strings.Fields(action)
	if len(words) == 0 {
		return "ALTER"
	}
	name := strings.ToUpper(words[0])
	if len(words) > 1 {
		switch next := strings.ToUpper(words[1]); next {
		case "COLUMN", "CONSTRAINT":
			name += " " + next
		}
	}
	return name
}

func markKey(t *SchemaTable, name string) {
	for i := range t.Columns {
		if t.Columns[i].Name == name {
			t.Columns[i].Key = true
		}
	}
}

// identName folds an unquoted identifier to lower case, as Postgres does, and keeps
// a quoted one as written. A leading "public." is the default schema and is dropped.
func identName(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) && len(s) >= 2 {
		return strings.TrimPrefix(s[1:len(s)-1], "public.")
	}
	return strings.TrimPrefix(strings.ToLower(s), "public.")
}

func joinNames(list string) string {
	var names []string
	for _, c := range splitList(list) {
		names = append(names, identName(c))
	}
	return strings.Join(names, ",")
}

func joinTarget(table, cols string) string {
	if cols == "" {
		return table
	}
	return table + "." + cols
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// splitTop splits a list at commas outside parentheses and quotes. An unbalanced
// list is an error: the reader cannot know where an item ends.
func splitTop(body string) ([]string, error) {
	var (
		items []string
		depth int
		quote rune
		start int
	)
	for i, r := range body {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '(':
			depth++
		case r == ')':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unbalanced parentheses")
			}
		case r == ',' && depth == 0:
			items = append(items, strings.TrimSpace(body[start:i]))
			start = i + 1
		}
	}
	if depth != 0 || quote != 0 {
		return nil, fmt.Errorf("unbalanced parentheses or quotes")
	}
	if last := strings.TrimSpace(body[start:]); last != "" {
		items = append(items, last)
	}
	return items, nil
}

// sqlStatements splits a migration into statements. Comments are dropped. Semicolons
// inside quotes and inside $$ function bodies do not end a statement.
func sqlStatements(src string) []string {
	var (
		out    []string
		cur    strings.Builder
		quote  byte
		dollar bool
		line   bool
		block  bool
	)
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for i := 0; i < len(src); i++ {
		c := src[i]
		next := byte(0)
		if i+1 < len(src) {
			next = src[i+1]
		}
		switch {
		case line:
			if c == '\n' {
				line = false
				cur.WriteByte(c)
			}
		case block:
			if c == '*' && next == '/' {
				block = false
				i++
			}
		case quote != 0:
			cur.WriteByte(c)
			if c == quote {
				quote = 0
			}
		case dollar:
			if c == '$' && next == '$' {
				dollar = false
				cur.WriteString("$$")
				i++
			} else {
				cur.WriteByte(c)
			}
		case c == '-' && next == '-':
			line = true
			i++
		case c == '/' && next == '*':
			block = true
			i++
		case c == '\'' || c == '"':
			quote = c
			cur.WriteByte(c)
		case c == '$' && next == '$':
			dollar = true
			cur.WriteString("$$")
			i++
		case c == ';':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// clip keeps a diagnostic short; a whole migration statement is not a message.
func clip(s string) string {
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}
