# Dynamic Page Manager

## Overview

The Dynamic Page Manager provides a complete system for managing dynamic web pages in the Fortress platform. It consists of an agent and a set of tools that enable users to create, read, update, and delete HTML pages that can interact with the backend API.

## Components

- **Page Manager Agent**: An AI agent specialized in managing dynamic web pages
- **Page Management Tools**: Lua functions for interacting with the registry system

## Agent Capabilities

The Page Manager Agent can:

1. **List Pages**: View all available dynamic pages with their metadata
2. **Get Page**: Retrieve the full content and metadata of a specific page
3. **Create Page**: Create new dynamic HTML pages with custom content
4. **Update Page**: Modify existing page content or metadata
5. **Delete Page**: Remove pages that are no longer needed

## Page Structure

All pages are HTML documents that are loaded in iframes and communicate with the backend API. Each page:

- Is stored in the `fortress.pages` namespace
- Has metadata (title, name, icon)
- Contains HTML content including scripts
- Can access the backend API via proxy.js

## Page API Access

Pages can access the Fortress API using the provided proxy script:

```html
<script src="http://localhost:5173/proxy.js"></script>
<script>
  init().then(({api, config}) => {
    console.log('App API is ready', api, config);
    // Use api.navigate() or api.startChat() here
  }).catch((err) => {
    console.error('Failed to initialize app', err);
  });
</script>
```

## Example Page Templates

### Basic Page Template

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Page Title</title>
    <script src="http://localhost:5173/proxy.js"></script>
    <style>
        body {
            font-family: Arial, sans-serif;
            line-height: 1.6;
            color: #333;
            max-width: 800px;
            margin: 0 auto;
            padding: 20px;
        }
        /* Add more styles here */
    </style>
</head>
<body>
    <h1>Page Title</h1>
    
    <!-- Page content here -->
    
    <script>
        init().then(({api, config}) => {
            console.log('App API is ready', api, config);
            // Your code here
        }).catch((err) => {
            console.error('Failed to initialize app', err);
        });
    </script>
</body>
</html>
```

## Technical Notes

- Pages must include the proxy.js script to communicate with the backend
- All HTML must be valid and well-formed
- Each page has a unique ID in the format `fortress.pages:<name>-<timestamp>`
- The agent uses the claude-3-7-sonnet model with 8000 max tokens for comprehensive assistance