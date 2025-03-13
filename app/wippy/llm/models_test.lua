local models = require("models")

local function define_tests()
    describe("Models Library", function()
        -- Test data from _models.yaml
        local registry_entries = {
            ["gpt-4o"] = {
                id = "gpt-4o",
                kind = "registry.entry",
                meta = {
                    type = "llm.model",
                    name = "gpt-4o",
                    comment = "Fast, intelligent, flexible GPT model with text and image input capabilities",
                    capabilities = { "tool_use", "vision" }
                },
                provider_model = "gpt-4o-2024-11-20",
                max_tokens = 128000,
                output_tokens = 16384,
                pricing = {
                    input = 2.50,
                    output = 10.00,
                    cached_input = 1.25
                },
                handlers = {
                    generate = "wippy.llm.openai:text_generation",
                    structured_output = "wippy.llm.openai:structured_output",
                    call_tools = "wippy.llm.openai:tool_calling"
                }
            },
            ["claude-3-7-sonnet"] = {
                id = "claude-3-7-sonnet",
                kind = "registry.entry",
                meta = {
                    type = "llm.model",
                    name = "claude-3-7-sonnet",
                    comment = "Anthropic's most intelligent model with extended thinking capabilities for complex reasoning",
                    capabilities = { "tool_use", "vision", "thinking", "caching" }
                },
                provider_model = "claude-3-7-sonnet-20250219",
                max_tokens = 200000,
                output_tokens = 4096,
                pricing = {
                    input = 3.00,
                    output = 15.00
                },
                handlers = {
                    generate = "wippy.llm.claude:text_generation",
                    structured_output = "wippy.llm.claude:structured_output",
                    call_tools = "wippy.llm.claude:tool_calling"
                }
            },
            ["text-embedding-3-large"] = {
                id = "text-embedding-3-large",
                kind = "registry.entry",
                meta = {
                    type = "llm.embedding",
                    name = "text-embedding-3-large",
                    comment = "Most powerful embedding model for highest accuracy (64.6% on MTEB benchmark)",
                    capabilities = { "multilingual" },
                    mteb_performance = 64.6,
                    knowledge_cutoff = "September 2021",
                    model_family = "third-generation"
                },
                provider_model = "text-embedding-3-large",
                dimensions = 3072,
                max_tokens = 8191,
                pricing = {
                    input = 0.13,
                    pages_per_dollar = 9615
                },
                handlers = {
                    embeddings = "wippy.llm.openai:embeddings"
                }
            }
        }

        before_each(function()
            -- Create a mock registry with our test data
            local mock_registry = {
                snapshot = function()
                    return {
                        find = function(query)
                            local results = {}
                            
                            -- Basic filtering logic to mimic the registry's find operation
                            for id, entry in pairs(registry_entries) do
                                local matches = true
                                
                                -- Check kind
                                if query[".kind"] and entry.kind ~= query[".kind"] then
                                    matches = false
                                end
                                
                                -- Check meta.type
                                if query["meta.type"] and entry.meta.type ~= query["meta.type"] then
                                    matches = false
                                end
                                
                                -- Check provider_model if specified
                                if query.provider_model and entry.provider_model ~= query.provider_model then
                                    matches = false
                                end
                                
                                if matches then
                                    table.insert(results, entry)
                                end
                            end
                            
                            return results
                        end,
                        get = function(id)
                            return registry_entries[id]
                        end
                    }, nil
                end
            }
            
            -- Override the registry module with our mock
            package.loaded.registry = mock_registry
        end)
        
        after_each(function()
            -- Reset the registry module
            package.loaded.registry = nil
        end)

        it("should find a model by exact name", function()
            local model, err = models.find_by_name("gpt-4o")
            
            expect(err).to_be_nil()
            expect(model).not_to_be_nil()
            expect(model.name).to_equal("gpt-4o")
            expect(model.provider_model).to_equal("gpt-4o-2024-11-20")
            expect(#model.capabilities).to_equal(2)
            expect(model.handlers.generate).to_equal("wippy.llm.openai:text_generation")
        end)
        
        it("should find a model by provider model ID", function()
            local model, err = models.find_by_name("claude-3-7-sonnet-20250219")
            
            expect(err).to_be_nil()
            expect(model).not_to_be_nil()
            expect(model.name).to_equal("claude-3-7-sonnet")
            expect(model.provider_model).to_equal("claude-3-7-sonnet-20250219")
        end)
        
        it("should return an error for non-existent models", function()
            local model, err = models.find_by_name("non-existent-model")
            
            expect(model).to_be_nil()
            expect(err).not_to_be_nil()
            expect(err:match("No model found")).not_to_be_nil()
        end)
        
        it("should find models by capability", function()
            local models_with_thinking, err = models.find_by_capability("thinking")
            
            expect(err).to_be_nil()
            expect(#models_with_thinking).to_equal(1)
            expect(models_with_thinking[1].name).to_equal("claude-3-7-sonnet")
            
            local models_with_vision, err = models.find_by_capability("vision")
            expect(err).to_be_nil()
            expect(#models_with_vision).to_equal(2)
        end)
        
        it("should list all available models", function()
            local all_models, err = models.list_all()
            
            expect(err).to_be_nil()
            expect(#all_models).to_equal(3)
        end)
        
        it("should get model by provider model identifier", function()
            local model, err = models.get_by_provider_model("text-embedding-3-large")
            
            expect(err).to_be_nil()
            expect(model).not_to_be_nil()
            expect(model.name).to_equal("text-embedding-3-large")
            expect(model.dimensions).to_equal(3072)
        end)
        
        it("should check if a model has a capability", function()
            local has_vision, err = models.has_capability("gpt-4o", "vision")
            expect(err).to_be_nil()
            expect(has_vision).to_be_true()
            
            local has_thinking, err = models.has_capability("gpt-4o", "thinking")
            expect(err).to_be_nil()
            expect(has_thinking).to_be_false()
        end)
        
        it("should provide complete model cards with all metadata", function()
            local embedding_model, err = models.find_by_name("text-embedding-3-large")
            
            expect(err).to_be_nil()
            expect(embedding_model.mteb_performance).to_equal(64.6)
            expect(embedding_model.dimensions).to_equal(3072)
            expect(embedding_model.model_family).to_equal("third-generation")
            expect(embedding_model.knowledge_cutoff).to_equal("September 2021")
        end)
    end)
end

return require("test").run_cases(define_tests)