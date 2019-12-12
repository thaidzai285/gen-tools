package gen

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"text/template"

	"gido.vn/gic/libs/common.git/gen"
	"gido.vn/gic/libs/common.git/l"
	"gido.vn/gic/sqitch/scripts/gen/load"
	"gido.vn/gic/sqitch/scripts/gen/middlewares"
	"gido.vn/gic/sqitch/scripts/gen/models"
)

var (
	projectPath string
	gopath      string
	planName    string
	ll          = l.New()
)

// Exec ...
func Exec(inputPath string) {
	createNewSqitchPlan(startNewSqitchPlan())
	genSchemaDefinations := load.LoadSchemaDefination(inputPath, planName)
	middlewares.GenerateSQL(genSchemaDefinations, generateDeploySQLScript, genSchemaDefinations)
}

func getPlanIndex() string {
	var planIndex string
	sqitchPlanPath := gen.GetAbsPath("gic/sqitch/sqitch.plan")
	file, err := os.Open(sqitchPlanPath)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	var lastLine string

	for scanner.Scan() {
		lastLine = scanner.Text()
	}
	ll.Print("last line: ", lastLine)
	index, _ := strconv.ParseInt(lastLine[0:3], 10, 64)
	var prefixIndex string
	switch {
	case index >= 9:
		prefixIndex = "0"
		break
	case index >= 99:
		prefixIndex = ""
		break
	default:
		prefixIndex = "00"
		break
	}
	planIndex = prefixIndex + strconv.FormatInt(index+1, 10)

	return planIndex
}

func genPlanNamePrefix(planIndex string) string {
	return planIndex + "-"
}

func startNewSqitchPlan() string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Enter migrate plan name: (For example: %v)\n", genPlanNamePrefix(getPlanIndex())+"xxxxxx")
	planName, _ = reader.ReadString('\n')
	planName = strings.Replace(planName, "\n", "", -1)

	return planName
}

func createNewSqitchPlan(planName string) {
	cmd := exec.Command("sqitch", "add", planName, "-n", "Add schema "+planName)
	ll.Info("Run sqitch add plan... Done†")
	cmd.Run()
}

func copyAllYamlSchema() {
	path := "./scripts/gen/schema/"
	cmd := exec.Command("cp", "-a", path+"tables", path+".restricted")
	cmd.Run()
}

func generateDeploySQLScript(migrate *models.MigrateSchema) {
	var script string = `
BEGIN;

{{- range $index, $table := $.Tables}}
{{- if $table.Fields}}
CREATE TABLE IF NOT EXISTS {{$table.TableName}} (
{{- range $index, $field := $table.Fields}}
	{{$field.Name}} {{$field.Type}} 
{{- if eq $field.Primary true}} PRIMARY KEY {{- end}}
{{- if eq $field.NotNull true}} NOT NULL {{- end}}
{{- if eq $field.Unique true}} Unique {{- end}}{{$lengthMinusOne := lengthMinusOne $table.Fields}}{{- if lt $index $lengthMinusOne}},{{- end}}
{{- end}}
);
{{- end}}

{{- if $table.Indexs}}
{{- range $i, $index := $table.Indexs}}
CREATE {{- if eq $index.Unique true}} UNIQUE{{- end}} INDEX IF NOT EXISTS {{$index.Name}} ON "{{$table.TableName}}" USING {{$index.Using}} ({{$index.Key}});
{{- end}}
{{- end}}
{{- end}}

{{- range $index, $table := $.AlterTables}}
{{- range $fieldIndex, $field := $table.Fields}}
	{{- if ne $field.Field.OldName ""}}
ALTER TABLE IF EXISTS {{$table.Name}}
	RENAME COLUMN {{$field.Field.OldName}} TO {{$field.Field.Name}};
	{{- end}}
	{{- if $field.IsNewField}}
ALTER TABLE IF EXISTS {{$table.Name}}
	ADD COLUMN IF NOT EXISTS {{$field.Field.Name}} {{$field.Field.Type}};
	{{- end}}
	{{- if or $field.IsNotNullChanged $field.IsNewField}}
ALTER TABLE IF EXISTS {{$table.Name}}
	ALTER COLUMN {{$field.Field.Name}} {{- if $field.Field.NotNull}} SET{{else}} DROP{{- end}} NOT NULL;
	{{- end}}
	{{- if or $field.IsUniqueChanged $field.IsNewField}} 
ALTER TABLE IF EXISTS {{$table.Name}}
	{{- if $field.Field.Unique}}
	ADD CONSTRAINT {{$table.Name}}_{{$field.Field.Name}}_key UNIQUE ({{$field.Field.Name}}); 
	{{else}}
	DROP CONSTRAINT IF EXISTS {{$table.Name}}_{{$field.Field.Name}}_key CASCADE; 
	{{- end}}
	{{- end}}
{{- end}}
{{- end}}

/*-- TRIGGER BEGIN --*/
{{$.Triggers}}
/*-- TRIGGER END --*/

COMMIT;
`
	templateFuncMap := template.FuncMap{
		"lengthMinusOne": lengthMinusOne,
	}
	var buf bytes.Buffer
	tpl := template.Must(template.New("scripts").Funcs(templateFuncMap).Parse(script))
	tpl.Execute(&buf, &migrate)
	dir := gen.GetAbsPath("gic/sqitch/deploy/")
	absPath := gen.GetAbsPath(dir + "/" + planName + ".sql")
	err := ioutil.WriteFile(absPath, buf.Bytes(), os.ModePerm)
	if err != nil {
		ll.Error("Error write file failed, %v\n", l.Error(err))
	}
	copyAllYamlSchema()
	ll.Info("==> Generate migrate deploy DONE†")
}

func generateRevertSQLScript(migrate *models.MigrateSchema) {
	var script string = `
{{- range $tableIndex, $table := $.Tables}}
DROP TABLE IF EXISTS {{$table.TableName}} CASCADE
{{- range $i, $index := $table.Indexs}}
DROP INDEX CONCURRENTLY IF EXISTS {{$index.Name}} CASCADE
{{- end}}
{{- end}}

{{- range $index, $table := $.AlterTables}}
{{- range $fieldIndex, $field := $table.Fields}}
	{{- if ne $field.Field.OldName ""}}
ALTER TABLE IF EXISTS {{$table.Name}}
	RENAME COLUMN {{$field.Field.Name}} TO {{$field.Field.OldName}};
	{{- end}}
	{{- if $field.IsNewField}}
ALTER TABLE IF EXISTS {{$table.Name}}
	DROP COLUMN IF EXISTS {{$field.Field.Name}};
	{{- end}}
	{{- if or $field.IsNotNullChanged $field.IsNewField}}
ALTER TABLE IF EXISTS {{$table.Name}}
	ALTER COLUMN {{$field.Field.Name}} {{- if $field.Field.NotNull}} SET{{else}} DROP{{- end}} NOT NULL;
	{{- end}}
	{{- if or $field.IsUniqueChanged $field.IsNewField}} 
ALTER TABLE IF EXISTS {{$table.Name}}
	{{- if $field.Field.Unique}}
	DROP CONSTRAINT IF EXISTS {{$table.Name}}_{{$field.Field.Name}}_key CASCADE;
	ADD CONSTRAINT {{$table.Name}}_{{$field.Field.Name}}_key UNIQUE ({{$field.Field.Name}}); 
	{{else}} 
	{{- end}}
	{{- end}}
{{- end}}
{{- end}}
`

	tpl := template.Must(template.New("scripts").Parse(script))
	tpl.Execute(os.Stdout, &migrate)
}

func lengthMinusOne(input interface{}) int {
	return reflect.ValueOf(input).Len() - 1
}
