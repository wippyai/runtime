//go:build fts5 && sqlite_vec
// +build fts5,sqlite_vec

package sql

import (
	"database/sql"
	"fmt"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"

	sqlapi "github.com/wippyai/runtime/api/service/sql"
	sqlres "github.com/wippyai/runtime/service/sql"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func init() {
	// Auto-register sqlite-vec extension
	sqlite_vec.Auto()

	// Log initialization for debugging purposes
	fmt.Println("SQLite Vector extension initialized")
}

// TestVectorWithSQLite tests using vectors with SQLite vec0 extension
func TestVectorWithSQLite(t *testing.T) {
	// Check if SQLite is available
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("SQLite not available")
	}
	defer db.Close()

	// Try to initialize a virtual table with vec0 (this will fail if extension not loaded)
	_, err = db.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS _test_vec_check USING vec0(v_id INTEGER PRIMARY KEY, v float[2])")
	if err != nil {
		t.Skip("SQLite vector extension not available: " + err.Error())
	}

	// Clean up test table
	db.Exec("DROP TABLE IF EXISTS _test_vec_check")

	// Setup test environment with the tested DB
	mockRes := &mockResource{
		resValue: sqlres.DBResource{
			DB:   db,
			Type: sqlapi.KindSQLite,
		},
	}

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Import test script
	script := `
		function test_vector_sqlite()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error("Failed to get database: " .. err) end
			
			-- Try to create a virtual table for vectors
			-- Note the explicit item_id INTEGER PRIMARY KEY 
			local ok, err = db:execute([[
				CREATE VIRTUAL TABLE IF NOT EXISTS vec_test_items USING vec0(
					item_id INTEGER PRIMARY KEY,
					embedding float[4],
					label TEXT
				)
			]])
			if err then return "sqlite-vec not available: " .. err end
			
			-- Insert test vectors using bracket notation format
			local test_vectors = {
				{id = 1, vec = "[0.1, 0.1, 0.1, 0.1]", label = "item1"},
				{id = 2, vec = "[0.2, 0.2, 0.2, 0.2]", label = "item2"},
				{id = 3, vec = "[0.3, 0.3, 0.3, 0.3]", label = "item3"},
				{id = 4, vec = "[0.4, 0.4, 0.4, 0.4]", label = "item4"},
				{id = 5, vec = "[0.5, 0.5, 0.5, 0.5]", label = "item5"}
			}
			
			for _, item in ipairs(test_vectors) do
				local res, err = db:execute(
					"INSERT INTO vec_test_items(item_id, embedding, label) VALUES (CAST(? AS INTEGER), ?, ?)",
					{item.id, item.vec, item.label}
				)
				if err then error("Failed to insert vector: " .. err) end
			end
			
			-- Perform KNN query using bracket notation for the query vector
			local query_vec = "[0.3, 0.3, 0.3, 0.3]"
			
			-- Perform KNN query
			local rows, err = db:query([[
				SELECT
					item_id,
					label,
					distance
				FROM vec_test_items
				WHERE embedding MATCH ?
				AND k = 3
				ORDER BY distance
			]], {query_vec})
			
			if err then error("Failed to execute KNN query: " .. err) end
			
			local results = {
				num_results = #rows,
				closest_id = rows[1] and rows[1].item_id or nil,
				closest_label = rows[1] and rows[1].label or nil,
				distances = {}
			}
			
			for i, row in ipairs(rows) do
				results.distances[i] = row.distance
			end
			
			-- Clean up test table
			db:execute("DROP TABLE vec_test_items")
			
			return results
		end
	`

	err = vm.Import(script, "test", "test_vector_sqlite")
	require.NoError(t, err, "Failed to import test script")

	// Execute the test function
	result, err := runner.Execute(ctx, "test_vector_sqlite")
	require.NoError(t, err, "Failed to execute test function")

	// If we got a string result, it means sqlite-vec is not available
	if str, ok := result.(lua.LString); ok {
		t.Skip("Skipping test: " + string(str))
		return
	}

	// Check results
	resultTable, ok := result.(*lua.LTable)
	require.True(t, ok, "Expected table result")

	numResults := resultTable.RawGetString("num_results").(lua.LNumber)
	closestID := resultTable.RawGetString("closest_id").(lua.LNumber)
	closestLabel := resultTable.RawGetString("closest_label").(lua.LString)

	assert.Equal(t, float64(3), float64(numResults), "Should have 3 results")
	assert.Equal(t, float64(3), float64(closestID), "Closest vector should be ID 3")
	assert.Equal(t, "item3", string(closestLabel), "Closest vector should be item3")
}

// TestHybridSearch tests combined vector search and FTS5 full-text search with BM25 ranking
func TestHybridSearch(t *testing.T) {
	// Check if SQLite is available
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("SQLite not available")
	}
	defer db.Close()

	// Try to initialize a virtual table with vec0 (this will fail if extension not loaded)
	_, err = db.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS _test_vec_check USING vec0(v_id INTEGER PRIMARY KEY, v float[2])")
	if err != nil {
		t.Skip("SQLite vector extension not available: " + err.Error())
	}

	// Try to initialize an FTS5 table (this will fail if FTS5 not enabled)
	_, err = db.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS _test_fts_check USING fts5(content)")
	if err != nil {
		t.Skip("SQLite FTS5 extension not available: " + err.Error())
	}

	// Clean up test tables
	db.Exec("DROP TABLE IF EXISTS _test_vec_check")
	db.Exec("DROP TABLE IF EXISTS _test_fts_check")

	// Setup test environment with the tested DB
	mockRes := &mockResource{
		resValue: sqlres.DBResource{
			DB:   db,
			Type: sqlapi.KindSQLite,
		},
	}

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Import test script for hybrid search that combines vector similarity and text relevance
	script := `
		function test_hybrid_search()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error("Failed to get database: " .. err) end
			
			-- Check SQLite version and capabilities
			local version_info, err = db:query("SELECT sqlite_version() as version")
			if err then error("Failed to get SQLite version: " .. err) end
			print("Using SQLite version: " .. version_info[1].version)
			
			-- Create tables for our test
			
			-- Create vector search table
			local ok, err = db:execute([[
				CREATE VIRTUAL TABLE IF NOT EXISTS vec_documents USING vec0(
					doc_id INTEGER PRIMARY KEY,
					embedding float[4],
					category TEXT,       -- metadata for filtering
					+title TEXT,         -- auxiliary column
					+summary TEXT        -- auxiliary column
				)
			]])
			if err then return "sqlite-vec not available: " .. err end
			
			-- Create FTS5 table for full-text search with BM25 ranking
			local ok, err = db:execute([[
				CREATE VIRTUAL TABLE IF NOT EXISTS docs_content USING fts5(
					doc_id UNINDEXED,    -- link to vector table
					title,               -- indexed for full-text search
					content,             -- indexed for full-text search
					summary              -- indexed for full-text search
				)
			]])
			if err then return "FTS5 not available: " .. err end
			
			-- Insert test documents
			local test_docs = {
				{
					id = 1, 
					vec = "[0.1, 0.1, 0.1, 0.1]", 
					category = "technology", 
					title = "Introduction to Neural Networks", 
					summary = "An overview of neural network architectures",
					content = "Neural networks are computational systems inspired by the human brain. They consist of layers of nodes that process data in a hierarchical manner. Deep learning models use multiple layers to extract increasingly complex features from raw input data."
				},
				{
					id = 2, 
					vec = "[0.2, 0.2, 0.2, 0.2]", 
					category = "technology", 
					title = "Understanding Vector Databases", 
					summary = "How vector databases enable semantic search",
					content = "Vector databases store high-dimensional vectors that represent semantic meaning. These databases enable similarity search which is essential for modern AI applications like recommendation systems and semantic search engines."
				},
				{
					id = 3, 
					vec = "[0.3, 0.3, 0.3, 0.3]", 
					category = "science", 
					title = "Quantum Computing Basics", 
					summary = "Introduction to quantum bits and gates",
					content = "Quantum computing uses quantum bits or qubits which can exist in multiple states simultaneously due to superposition. Quantum gates manipulate qubits to perform computations that would be impractical on classical computers."
				},
				{
					id = 4, 
					vec = "[0.4, 0.4, 0.4, 0.4]", 
					category = "history", 
					title = "Ancient Greek Philosophy", 
					summary = "Overview of Socrates, Plato, and Aristotle",
					content = "Greek philosophy laid the foundations for Western thought. Key figures include Socrates who developed the Socratic method, Plato who wrote dialogues on justice and knowledge, and Aristotle who contributed to logic, metaphysics, and ethics."
				},
				{
					id = 5, 
					vec = "[0.5, 0.5, 0.5, 0.5]", 
					category = "technology", 
					title = "Machine Learning Algorithms", 
					summary = "Survey of common machine learning approaches",
					content = "Machine learning algorithms include supervised learning techniques like decision trees and neural networks, unsupervised methods like clustering, and reinforcement learning which trains agents through rewards and penalties."
				},
				{
					id = 6, 
					vec = "[0.35, 0.32, 0.31, 0.34]", 
					category = "science", 
					title = "Introduction to Quantum Machine Learning", 
					summary = "Where quantum computing meets AI",
					content = "Quantum machine learning combines quantum computing with machine learning techniques. It offers potential speedups for certain algorithms and may enable new approaches to optimization, classification, and data analysis."
				},
			}
			
			for _, doc in ipairs(test_docs) do
				-- Insert into vector table
				local res, err = db:execute(
					"INSERT INTO vec_documents(doc_id, embedding, category, title, summary) VALUES (CAST(? AS INTEGER), ?, ?, ?, ?)",
					{doc.id, doc.vec, doc.category, doc.title, doc.summary}
				)
				if err then error("Failed to insert into vector table: " .. err) end
				
				-- Insert into FTS5 table
				local res, err = db:execute(
					"INSERT INTO docs_content(doc_id, title, content, summary) VALUES (?, ?, ?, ?)",
					{doc.id, doc.title, doc.content, doc.summary}
				)
				if err then error("Failed to insert into FTS5 table: " .. err) end
			end
			
			local results = {}
			
			-- Test 1: Simple vector similarity search
			local query_vec = "[0.3, 0.3, 0.3, 0.3]"
			
			-- Run vector search
			local rows, err = db:query([[
				SELECT 
					doc_id,
					title,
					summary,
					distance
				FROM vec_documents
				WHERE embedding MATCH ?
				AND k = 3
				ORDER BY distance
			]], {query_vec})
			
			if err then error("Failed vector search: " .. err) end
			
			results.vector_search = {
				count = #rows,
				closest_id = rows[1].doc_id,
				closest_title = rows[1].title
			}
			
			-- Test 2: Full-text search with BM25 ranking
			local search_term = "quantum"
			local rows, err = db:query([[
				SELECT 
					doc_id,
					title,
					highlight(docs_content, 1, '<b>', '</b>') AS title_highlighted,
					highlight(docs_content, 2, '<b>', '</b>') AS content_highlighted,
					bm25(docs_content) AS relevance
				FROM docs_content
				WHERE docs_content MATCH ?
				ORDER BY relevance
			]], {search_term})
			
			if err then error("Failed full-text search: " .. err) end
			
			results.text_search = {
				count = #rows,
				doc_ids = {},
				relevance = {}
			}
			
			for i, row in ipairs(rows) do
				table.insert(results.text_search.doc_ids, row.doc_id)
				results.text_search.relevance[i] = row.relevance
			end
			
			-- Test 3: Hybrid search - combine vector similarity with text relevance
			local rows, err = db:query([[
				WITH vector_matches AS (
					SELECT 
						doc_id,
						distance
					FROM vec_documents
					WHERE embedding MATCH ?
					AND k = 5
				),
				text_matches AS (
					SELECT 
						doc_id,
						bm25(docs_content) AS relevance
					FROM docs_content
					WHERE docs_content MATCH ?
				)
				SELECT 
					v.doc_id,
					d.title,
					d.summary,
					v.distance AS vector_distance,
					t.relevance AS text_relevance,
					-- Hybrid ranking formula (normalize and combine scores)
					(1 - (v.distance / 2)) * 0.5 + (1 / (t.relevance + 1)) * 0.5 AS hybrid_score
				FROM vector_matches v
				JOIN text_matches t ON v.doc_id = t.doc_id
				JOIN vec_documents d ON v.doc_id = d.doc_id
				ORDER BY hybrid_score DESC
				LIMIT 3
			]], {query_vec, search_term})
			
			if err then error("Failed hybrid search: " .. err) end
			
			results.hybrid_search = {
				count = #rows,
				doc_ids = {},
				scores = {}
			}
			
			for i, row in ipairs(rows) do
				table.insert(results.hybrid_search.doc_ids, row.doc_id)
				results.hybrid_search.scores[i] = {
					vector_distance = row.vector_distance,
					text_relevance = row.text_relevance,
					hybrid_score = row.hybrid_score
				}
			end
			
			-- Test 4: Vector search with metadata filtering
			local rows, err = db:query([[
				SELECT 
					doc_id,
					title,
					category,  -- Add category to SELECT
					summary,
					distance
				FROM vec_documents
				WHERE embedding MATCH ?
				AND category = 'technology'
				AND k = 3
				ORDER BY distance
			]], {query_vec})
			
			if err then error("Failed filtered vector search: " .. err) end
			
			results.filtered_search = {
				count = #rows,
				category_matches = true
			}
			
			for _, row in ipairs(rows) do
				if row.category ~= 'technology' then
					results.filtered_search.category_matches = false
					print("Found non-technology category: " .. (row.category or "nil"))
				end
			end
			
			-- Clean up test tables
			db:execute("DROP TABLE IF EXISTS vec_documents")
			db:execute("DROP TABLE IF EXISTS docs_content")
			
			return results
		end
	`

	err = vm.Import(script, "test", "test_hybrid_search")
	require.NoError(t, err, "Failed to import test script")

	// Execute the test function
	result, err := runner.Execute(ctx, "test_hybrid_search")
	require.NoError(t, err, "Failed to execute hybrid search test function")

	// If we got a string result, it means either sqlite-vec or FTS5 is not available
	if str, ok := result.(lua.LString); ok {
		t.Skip("Skipping test: " + string(str))
		return
	}

	// Check results
	resultTable, ok := result.(*lua.LTable)
	require.True(t, ok, "Expected table result")

	// Vector search results
	vectorSearch := resultTable.RawGetString("vector_search").(*lua.LTable)
	vectorCount := vectorSearch.RawGetString("count").(lua.LNumber)
	assert.Equal(t, float64(3), float64(vectorCount), "Should have 3 vector search results")

	// Text search results
	textSearch := resultTable.RawGetString("text_search").(*lua.LTable)
	textCount := textSearch.RawGetString("count").(lua.LNumber)
	assert.True(t, float64(textCount) > 0, "Should have text search results")

	// Hybrid search results
	hybridSearch := resultTable.RawGetString("hybrid_search").(*lua.LTable)
	hybridCount := hybridSearch.RawGetString("count").(lua.LNumber)
	assert.True(t, float64(hybridCount) > 0, "Should have hybrid search results")

	// Test that filtered search only returns technology category
	filteredSearch := resultTable.RawGetString("filtered_search").(*lua.LTable)
	categoryMatches := filteredSearch.RawGetString("category_matches").(lua.LBool)
	assert.True(t, bool(categoryMatches), "Filtered search should only return technology category")
}
