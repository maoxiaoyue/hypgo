## Why Do We Need Another Go Web Framework? The Birth of HypGo

In the world of Go, there is no shortage of high-performance web frameworks. From Gin's rapid routing to Echo's extensibility, and Fiber's friendliness to ExpressJS developers, the ecosystem is vibrant and diverse. However, after developing several projects, our team noticed some recurring "friction points." These issues not only slowed down development but also introduced technical debt in the later stages of our projects.

We believe that a modern framework should not just be "usable"; it should guide developers toward a more robust and forward-thinking architecture right from the **design and conceptualization phase**. This is why we created HypGo.

HypGo aims to solve three core problems we've observed:

  * **Absence of Modern Network Protocols**: Support for HTTP/2 and HTTP/3 is often an afterthought.
  * **The "Lightweight" Paradox of Choice**: The framework itself is minimal, but the burden of integrating common functionalities is left entirely to the developer.
  * **A Fragmented Integration Experience**: Various modules feel like a loose collection of parts rather than a tightly cohesive whole.

### 1\. Built-in Modern Asynchronous I/O Model, Native Support for HTTP/1\~HTTP/3

Have you ever experienced this? Months into a project, during performance testing, you realize that to fully leverage the multiplexing advantages of modern browsers, you must enable HTTP/2. Or, in pursuit of lower latency, the team decides to upgrade to HTTP/3. It's only then that you discover your current framework requires a series of complex configurations, or even the introduction of third-party packages, just to get it working.

This "add-it-later" approach is fraught with risk. It's not only time-consuming but can also create conflicts with your existing architecture.

We believe that support for modern network protocols should be a **built-in capability** of the framework, not a plugin that needs to be installed separately. In HypGo, enabling HTTP/3 is incredibly simple:

```go
package main

import "github.com/maoxiaoyue/HypGo"

func main() {
    app := HypGo.New()

    app.Get("/", func(c *HypGo.Ctx) error {
        return c.SendString("Hello, World!")
    })

    // Start an HTTP/3 server with automatic fallback to HTTP/2 and HTTP/1.1.
    // It's that simple!
    if err := app.Listen(":443", "./cert.pem", "./key.pem"); err != nil {
		panic(err)
	}
}
```

We have encapsulated the complex underlying processes, allowing you to confidently adopt the most advanced network technologies from day one, rather than waiting until it becomes a last-minute emergency.

### 2\. From "Lightweight" to "Just Right": Say Goodbye to Analysis Paralysis

Many frameworks pride themselves on being "lightweight," which is certainly appealing at first. But "lightweight" often means "empty." When you need to handle databases, configurations, validation, logging, and other real-world business requirements, you are immediately thrown into an "ocean of choices":

  * For logging, should I use `logrus`, `zap`, or `zerolog`?
  * For an ORM, should I choose `GORM`, `sqlx`, or `ent`? How do they interact with the framework's context?
  * For configuration management, should I use `Viper` or native flags? How can I load them elegantly into the application?

This model shifts the heavy responsibility of architectural integration entirely onto the developer. Developers must not only spend a great deal of time researching and evaluating options but also personally glue these components from different communities together, hoping they will coexist harmoniously.

HypGo chooses a different path: **providing a curated and deeply integrated set of recommended solutions**. We are not trying to build a monolithic, all-in-one framework, but rather to offer a "just right" development experience. You can use these built-in solutions for out-of-the-box convenience, or easily swap them out if you have special requirements. Our goal is to eliminate unnecessary decision fatigue, allowing you to focus on business logic.

### 3\. Integration, Not Patchwork: Building a Seamless Development Experience

A lack of integration is the inevitable result of the first two problems. When you manually piece together logging, routing, ORM, and configuration modules, they often remain disconnected from one another.

  * Your ORM is unaware of the request timeout set at the routing layer.
  * Your logging module doesn't know who the current user is.
  * When your configuration changes, there is no smooth way to notify all corners of the application.

This leads to a large amount of boilerplate code and inconsistent behavior.

The core design philosophy of HypGo is **deep integration**. All core components are designed to "talk" to each other.

  * **Unified Context**: Our `Ctx` (Context) flows through the entire request lifecycle—from middleware and handlers to database operations. You can easily pass data, control timeouts, and handle cancellation signals.
  * **Dependency Injection**: We provide a clean dependency injection container, making the management and use of services (like database connection pools or cache clients) simpler and clearer than ever.

In the end, what you get is not just a collection of features, but a **highly cohesive, loosely coupled** application platform.

## Our Vision: Modern from the Start

We firmly believe that a great framework should act like an experienced architect, laying out a clear path to success for you from the very beginning of a project.

The mission of HypGo is to empower every Go developer to easily and confidently **adopt better, more modern solutions right from the project's design and conceptualization phase**, building truly future-proof applications.

We sincerely invite you to try HypGo and join our community. Whether it's submitting an issue, opening a pull request, or simply giving us a star, your support means the world to us\!

[**GitHub Repository**](https://www.google.com/search?q=https://github.com/maoxiaoyue/HypGo) | [**Quick Start Guide**](https://www.google.com/search?q=https://github.com/maoxiaoyue/HypGo/wiki)
