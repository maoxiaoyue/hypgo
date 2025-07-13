package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
)

var generateCmd = &cobra.Command{
	Use:   "generate [type] [name]",
	Short: "Generate code (controller, model, service)",
	Args:  cobra.ExactArgs(2),
	RunE:  runGenerate,
}

func runGenerate(cmd *cobra.Command, args []string) error {
	genType := args[0]
	name := args[1]

	switch genType {
	case "controller":
		return generateController(name)
	case "model":
		return generateModel(name)
	case "service":
		return generateService(name)
	default:
		return fmt.Errorf("unknown type: %s (use controller, model, or service)", genType)
	}
}

func generateController(name string) error {
	tmpl := `package controllers

import (
    "encoding/json"
    "net/http"

    "github.com/gorilla/mux"
    "github.com/maoxiaoyue/hypgo/pkg/database"
    "github.com/maoxiaoyue/hypgo/pkg/logger"
)

type {{.Name}}Controller struct {
    db     *database.Database
    logger *logger.Logger
}

func New{{.Name}}Controller(db *database.Database, logger *logger.Logger) *{{.Name}}Controller {
    return &{{.Name}}Controller{
        db:     db,
        logger: logger,
    }
}

func (c *{{.Name}}Controller) RegisterRoutes(router *mux.Router) {
    router.HandleFunc("/api/{{.LowerName}}", c.List).Methods("GET")
    router.HandleFunc("/api/{{.LowerName}}", c.Create).Methods("POST")
    router.HandleFunc("/api/{{.LowerName}}/{id}", c.Get).Methods("GET")
    router.HandleFunc("/api/{{.LowerName}}/{id}", c.Update).Methods("PUT")
    router.HandleFunc("/api/{{.LowerName}}/{id}", c.Delete).Methods("DELETE")
}

func (c *{{.Name}}Controller) List(w http.ResponseWriter, r *http.Request) {
    c.logger.Info("List {{.Name}} called")
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(Response{
        Success: true,
        Message: "{{.Name}} list",
        Data:    []string{},
    })
}

func (c *{{.Name}}Controller) Create(w http.ResponseWriter, r *http.Request) {
    c.logger.Info("Create {{.Name}} called")
    
    // TODO: Implement create logic
    
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(Response{
        Success: true,
        Message: "{{.Name}} created",
    })
}

func (c *{{.Name}}Controller) Get(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    id := vars["id"]
    
    c.logger.Info("Get {{.Name}} called with ID: %s", id)
    
    // TODO: Implement get logic
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(Response{
        Success: true,
        Message: "{{.Name}} details",
        Data:    map[string]string{"id": id},
    })
}

func (c *{{.Name}}Controller) Update(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    id := vars["id"]
    
    c.logger.Info("Update {{.Name}} called with ID: %s", id)
    
    // TODO: Implement update logic
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(Response{
        Success: true,
        Message: "{{.Name}} updated",
    })
}

func (c *{{.Name}}Controller) Delete(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    id := vars["id"]
    
    c.logger.Info("Delete {{.Name}} called with ID: %s", id)
    
    // TODO: Implement delete logic
    
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusNoContent)
}
`

	return generateFile("app/controllers", name+"_controller.go", tmpl, map[string]string{
		"Name":      capitalize(name),
		"LowerName": strings.ToLower(name),
	})
}

func generateModel(name string) error {
	tmpl := `package models

import (
    "time"
    
    "entgo.io/ent"
    "entgo.io/ent/schema/field"
)

// {{.Name}} holds the schema definition for the {{.Name}} entity.
type {{.Name}} struct {
    ent.Schema
}

// Fields of the {{.Name}}.
func ({{.Name}}) Fields() []ent.Field {
    return []ent.Field{
        field.String("name").
            NotEmpty(),
        field.Text("description").
            Optional(),
        field.Bool("active").
            Default(true),
        field.Time("created_at").
            Default(time.Now),
        field.Time("updated_at").
            Default(time.Now).
            UpdateDefault(time.Now),
    }
}

// Edges of the {{.Name}}.
func ({{.Name}}) Edges() []ent.Edge {
    return nil
}
`

	return generateFile("app/models", strings.ToLower(name)+".go", tmpl, map[string]string{
		"Name": capitalize(name),
	})
}

func generateService(name string) error {
	tmpl := `package services

import (
    "context"
    "fmt"

    "github.com/maoxiaoyue/hypgo/pkg/database"
    "github.com/maoxiaoyue/hypgo/pkg/logger"
)

type {{.Name}}Service struct {
    db     *database.Database
    logger *logger.Logger
}

func New{{.Name}}Service(db *database.Database, logger *logger.Logger) *{{.Name}}Service {
    return &{{.Name}}Service{
        db:     db,
        logger: logger,
    }
}

func (s *{{.Name}}Service) Create(ctx context.Context, data map[string]interface{}) error {
    s.logger.Info("Creating new {{.LowerName}}")
    
    // TODO: Implement create logic
    
    return nil
}

func (s *{{.Name}}Service) GetByID(ctx context.Context, id string) (map[string]interface{}, error) {
    s.logger.Info("Getting {{.LowerName}} by ID: %s", id)
    
    // TODO: Implement get logic
    
    return map[string]interface{}{
        "id": id,
    }, nil
}

func (s *{{.Name}}Service) Update(ctx context.Context, id string, data map[string]interface{}) error {
    s.logger.Info("Updating {{.LowerName}} ID: %s", id)
    
    // TODO: Implement update logic
    
    return nil
}

func (s *{{.Name}}Service) Delete(ctx context.Context, id string) error {
    s.logger.Info("Deleting {{.LowerName}} ID: %s", id)
    
    // TODO: Implement delete logic
    
    return nil
}

func (s *{{.Name}}Service) List(ctx context.Context, filters map[string]interface{}) ([]map[string]interface{}, error) {
    s.logger.Info("Listing {{.LowerName}}s with filters: %v", filters)
    
    // TODO: Implement list logic
    
    return []map[string]interface{}{}, nil
}
`

	return generateFile("app/services", strings.ToLower(name)+"_service.go", tmpl, map[string]string{
		"Name":      capitalize(name),
		"LowerName": strings.ToLower(name),
	})
}

func generateFile(dir, filename, tmplStr string, data interface{}) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	tmpl, err := template.New("generate").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	filepath := filepath.Join(dir, filename)
	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	if err := tmpl.Execute(file, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	fmt.Printf("✅ Generated: %s\n", filepath)
	return nil
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
