package fixture

import (
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
)

func setupRouter() *router.Router {
	r := router.New()
	r.GET("/health", func(c *hypcontext.Context) {
		c.JSON(200, map[string]string{"status": "ok"})
	})
	r.POST("/api/users", func(c *hypcontext.Context) {
		c.JSON(201, map[string]interface{}{
			"id":   1,
			"name": "test",
		})
	})
	r.GET("/api/users/:id", func(c *hypcontext.Context) {
		c.JSON(200, map[string]interface{}{
			"id": c.Param("id"),
		})
	})
	r.DELETE("/api/users/:id", func(c *hypcontext.Context) {
		c.Status(204)
		c.Writer.WriteHeaderNow()
	})
	return r
}

func TestRequestGET(t *testing.T) {
	r := setupRouter()
	result := Request(r).GET("/health").Expect(200).Run(t)

	if result.Status != 200 {
		t.Errorf("status = %d, want 200", result.Status)
	}

	m, err := result.JSONMap()
	if err != nil {
		t.Fatal(err)
	}
	if m["status"] != "ok" {
		t.Errorf("status = %v, want ok", m["status"])
	}
}

func TestRequestPOSTWithJSON(t *testing.T) {
	r := setupRouter()
	result := Request(r).
		POST("/api/users").
		WithJSON(map[string]string{"name": "alice"}).
		Expect(201).
		Run(t)

	var resp map[string]interface{}
	if err := result.JSON(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["name"] != "test" {
		t.Errorf("name = %v", resp["name"])
	}
}

func TestRequestWithHeader(t *testing.T) {
	r := setupRouter()
	result := Request(r).
		GET("/health").
		WithHeader("X-Custom", "test").
		Expect(200).
		Run(t)

	if result.Status != 200 {
		t.Error("should pass with custom header")
	}
}

func TestRequestWithQuery(t *testing.T) {
	r := router.New()
	r.GET("/search", func(c *hypcontext.Context) {
		q := c.Query("q")
		c.JSON(200, map[string]string{"query": q})
	})

	result := Request(r).
		GET("/search").
		WithQuery("q", "hello").
		Expect(200).
		Run(t)

	m, _ := result.JSONMap()
	if m["query"] != "hello" {
		t.Errorf("query = %v, want hello", m["query"])
	}
}

func TestRequestDELETE(t *testing.T) {
	r := setupRouter()
	result := Request(r).DELETE("/api/users/1").Expect(204).Run(t)
	if result.Status != 204 {
		t.Errorf("status = %d, want 204", result.Status)
	}
}

func TestRequest404(t *testing.T) {
	r := setupRouter()
	result := Request(r).GET("/nonexistent").Expect(404).Run(t)
	if result.Status != 404 {
		t.Errorf("status = %d, want 404", result.Status)
	}
}

func TestRequestPUT(t *testing.T) {
	r := router.New()
	r.PUT("/api/items/:id", func(c *hypcontext.Context) {
		c.JSON(200, map[string]string{"updated": c.Param("id")})
	})

	result := Request(r).
		PUT("/api/items/42").
		WithJSON(map[string]string{"name": "updated"}).
		Expect(200).
		Run(t)

	m, _ := result.JSONMap()
	if m["updated"] != "42" {
		t.Errorf("updated = %v, want 42", m["updated"])
	}
}

func TestRequestPATCH(t *testing.T) {
	r := router.New()
	r.PATCH("/api/items/:id", func(c *hypcontext.Context) {
		c.JSON(200, map[string]string{"patched": "true"})
	})

	result := Request(r).PATCH("/api/items/1").Expect(200).Run(t)
	if result.Status != 200 {
		t.Error("PATCH should succeed")
	}
}

func TestRequestWithBody(t *testing.T) {
	r := router.New()
	r.POST("/raw", func(c *hypcontext.Context) {
		c.JSON(200, map[string]string{"ok": "true"})
	})

	result := Request(r).
		POST("/raw").
		WithBody("raw body content").
		Expect(200).
		Run(t)

	if result.Status != 200 {
		t.Error("raw body should work")
	}
}

// --- TestResult methods ---

func TestResultBodyString(t *testing.T) {
	r := setupRouter()
	result := Request(r).GET("/health").Run(t)
	body := result.BodyString()
	if body == "" {
		t.Error("body should not be empty")
	}
}

func TestResultHasHeader(t *testing.T) {
	r := setupRouter()
	result := Request(r).GET("/health").Run(t)
	if !result.HasHeader("Content-Type") {
		t.Error("should have Content-Type header")
	}
	if result.HasHeader("X-Nonexistent") {
		t.Error("should not have X-Nonexistent header")
	}
}
