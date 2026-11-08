package analyze

import (
	"strings"
	"testing"
)

// schemaOf runs the reader over migrations given in file-name order.
func schemaOf(t *testing.T, migrations ...File) Schema {
	t.Helper()
	return ReadSchema(migrations)
}

func table(t *testing.T, s Schema, name string) SchemaTable {
	t.Helper()
	for _, tb := range s.Tables {
		if tb.Name == name {
			return tb
		}
	}
	t.Fatalf("table %q not found in %+v", name, s.Tables)
	return SchemaTable{}
}

// AC18: a table shows its columns and its references, and each relation names the
// migration that declared it.
func TestSchemaReadsTablesColumnsAndForeignKeys(t *testing.T) {
	s := schemaOf(t, File{Path: "db/0001.sql", Body: []byte(`
CREATE TABLE creator (
  id uuid PRIMARY KEY,
  name text NOT NULL
);

CREATE TABLE offer (
  id uuid PRIMARY KEY,
  creator_id uuid NOT NULL REFERENCES creator(id),
  price integer
);
`)})

	offer := table(t, s, "offer")
	if got := columnNames(offer); got != "id,creator_id,price" {
		t.Errorf("offer columns = %s", got)
	}
	if len(offer.References) != 1 {
		t.Fatalf("offer references = %+v", offer.References)
	}
	ref := offer.References[0]
	if ref.From != "offer.creator_id" || ref.To != "creator.id" || ref.Migration != "db/0001.sql" {
		t.Errorf("reference = %+v", ref)
	}
}

// AC18: migrations apply in file-name order, so a later ALTER TABLE adds to the
// table an earlier one created, and a dropped constraint is no longer a relation.
func TestSchemaAppliesMigrationsInOrder(t *testing.T) {
	s := schemaOf(t,
		File{Path: "db/0002_b.sql", Body: []byte(`
ALTER TABLE order_line ADD COLUMN sku text;
ALTER TABLE order_line ADD CONSTRAINT order_line_order_fk FOREIGN KEY (order_id) REFERENCES purchase(id);
ALTER TABLE order_line DROP CONSTRAINT order_line_gone_fk;
`),
		},
		File{Path: "db/0001_a.sql", Body: []byte(`
CREATE TABLE order_line (
  id uuid PRIMARY KEY,
  order_id uuid,
  CONSTRAINT order_line_gone_fk FOREIGN KEY (order_id) REFERENCES purchase(id)
);
CREATE TABLE purchase (id uuid PRIMARY KEY);
`),
		},
	)
	// ReadSchema must sort by path itself: the caller does not promise an order.
	line := table(t, s, "order_line")
	if got := columnNames(line); got != "id,order_id,sku" {
		t.Errorf("order_line columns = %s", got)
	}
	var kept []string
	for _, r := range line.References {
		kept = append(kept, r.Constraint)
	}
	if strings.Join(kept, ",") != "order_line_order_fk" {
		t.Errorf("references after drop = %v", kept)
	}
}

// AC18: a statement the reader does not model marks its table unknown, with a
// reason; the rest of the schema is still shown.
func TestUnmodelledStatementMarksTableUnknown(t *testing.T) {
	s := schemaOf(t, File{Path: "db/0001.sql", Body: []byte(`
CREATE TABLE ledger (id uuid PRIMARY KEY, amount integer);
ALTER TABLE ledger ALTER COLUMN amount TYPE numeric;
CREATE TABLE other (id uuid PRIMARY KEY);
`)})

	ledger := table(t, s, "ledger")
	if ledger.Unknown == "" || !strings.Contains(ledger.Unknown, "ALTER COLUMN") {
		t.Errorf("ledger unknown = %q, want a reason naming the statement", ledger.Unknown)
	}
	if other := table(t, s, "other"); other.Unknown != "" {
		t.Errorf("other table should stay known, got %q", other.Unknown)
	}
}

// Declarations inside a migration do not name real tables when they are in a
// comment or a function body: those must not appear as entities.
func TestCommentsAndFunctionBodiesAreNotTables(t *testing.T) {
	s := schemaOf(t, File{Path: "db/0001.sql", Body: []byte(`
-- CREATE TABLE ghost (id uuid);
CREATE FUNCTION reject() RETURNS trigger AS $$
BEGIN
  CREATE TABLE inside_body (id int);
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;
CREATE TABLE real (id uuid PRIMARY KEY);
`)})

	if len(s.Tables) != 1 || s.Tables[0].Name != "real" {
		t.Errorf("tables = %+v, want only real", s.Tables)
	}
}

// Migrations this reader does not read at all are reported, not skipped silently.
func TestSchemaDiagnosesAnUnreadableMigration(t *testing.T) {
	s := schemaOf(t, File{Path: "db/0001.sql", Body: []byte("CREATE TABLE (((\n")})
	if len(s.Diagnostics) == 0 {
		t.Fatal("an unreadable migration produced no diagnostic")
	}
}

func columnNames(tb SchemaTable) string {
	names := make([]string, 0, len(tb.Columns))
	for _, c := range tb.Columns {
		names = append(names, c.Name)
	}
	return strings.Join(names, ",")
}
