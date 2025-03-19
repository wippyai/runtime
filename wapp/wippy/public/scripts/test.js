// Test Case Component
const TestCase = {
    template: '#test-case-template',
    props: {
        test: {
            type: Object,
            required: true
        }
    },
    methods: {
        getStatusClass() {
            switch (this.test.status) {
                case 'passed':
                    return 'status-passed';
                case 'failed':
                    return 'status-failed';
                case 'running':
                    return 'status-running';
                case 'error':
                    return 'status-error';
                case 'skipped':
                    return 'status-skipped';
                default:
                    return 'status-pending';
            }
        },
        getStatusIcon() {
            switch (this.test.status) {
                case 'passed':
                    return `<svg xmlns="http://www.w3.org/2000/svg" class="icon text-success" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clip-rule="evenodd" />
                        </svg>`;
                case 'failed':
                    return `<svg xmlns="http://www.w3.org/2000/svg" class="icon text-danger" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd" />
                        </svg>`;
                case 'error':
                    return `<svg xmlns="http://www.w3.org/2000/svg" class="icon text-warning" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clip-rule="evenodd" />
                        </svg>`;
                case 'running':
                    return `<svg xmlns="http://www.w3.org/2000/svg" class="icon text-primary animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                            <circle class="opacity-25" cx="12" cy="12" r="10" stroke-width="4"></circle>
                            <path class="opacity-75" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                        </svg>`;
                case 'skipped':
                    return `<svg xmlns="http://www.w3.org/2000/svg" class="icon text-neutral" viewBox="0 0 20 20" fill="currentColor">
                            <path d="M10 6a2 2 0 110-4 2 2 0 010 4zM10 12a2 2 0 110-4 2 2 0 010 4zM10 18a2 2 0 110-4 2 2 0 010 4z" />
                        </svg>`;
                default:
                    return `<svg xmlns="http://www.w3.org/2000/svg" class="icon text-neutral" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm1-12a1 1 0 10-2 0v4a1 1 0 00.293.707l2.828 2.829a1 1 0 101.415-1.415L11 9.586V6z" clip-rule="evenodd" />
                        </svg>`;
            }
        }
    }
};

// Flat Test Item Component
const FlatTestItem = {
    template: '#flat-test-item-template',
    props: {
        test: {
            type: Object,
            required: true
        },
        suiteName: {
            type: String,
            required: true
        },
        opId: {
            type: String,
            required: true
        }
    },
    methods: {
        getStatusClass() {
            switch (this.test.status) {
                case 'passed':
                    return 'status-passed';
                case 'failed':
                    return 'status-failed';
                case 'running':
                    return 'status-running';
                case 'error':
                    return 'status-error';
                case 'skipped':
                    return 'status-skipped';
                default:
                    return 'status-pending';
            }
        },
        getStatusIcon() {
            switch (this.test.status) {
                case 'passed':
                    return `<svg xmlns="http://www.w3.org/2000/svg" class="icon text-success" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clip-rule="evenodd" />
                        </svg>`;
                case 'failed':
                    return `<svg xmlns="http://www.w3.org/2000/svg" class="icon text-danger" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd" />
                        </svg>`;
                case 'error':
                    return `<svg xmlns="http://www.w3.org/2000/svg" class="icon text-warning" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clip-rule="evenodd" />
                        </svg>`;
                case 'running':
                    return `<svg xmlns="http://www.w3.org/2000/svg" class="icon text-primary animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                            <circle class="opacity-25" cx="12" cy="12" r="10" stroke-width="4"></circle>
                            <path class="opacity-75" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                        </svg>`;
                case 'skipped':
                    return `<svg xmlns="http://www.w3.org/2000/svg" class="icon text-neutral" viewBox="0 0 20 20" fill="currentColor">
                            <path d="M10 6a2 2 0 110-4 2 2 0 010 4zM10 12a2 2 0 110-4 2 2 0 010 4zM10 18a2 2 0 110-4 2 2 0 010 4z" />
                        </svg>`;
                default:
                    return `<svg xmlns="http://www.w3.org/2000/svg" class="icon text-neutral" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm1-12a1 1 0 10-2 0v4a1 1 0 00.293.707l2.828 2.829a1 1 0 101.415-1.415L11 9.586V6z" clip-rule="evenodd" />
                        </svg>`;
            }
        }
    }
};

// Flat Test List Component
const FlatTestList = {
    template: '#flat-test-list-template',
    components: {
        FlatTestItem
    },
    props: {
        testItems: {
            type: Array,
            required: true
        }
    }
};

// Test Suite Component
const TestSuite = {
    template: '#test-suite-template',
    components: {
        TestCase
    },
    props: {
        suite: {
            type: Object,
            required: true
        }
    },
    data() {
        return {
            expanded: false
        };
    },
    methods: {
        toggleExpanded() {
            this.expanded = !this.expanded;
        },
        getSuiteHeaderClass() {
            if (this.suite.failed > 0) {
                return 'bg-danger-light';
            } else if (this.suite.passed > 0 && this.suite.passed + this.suite.skipped === this.suite.total) {
                return 'bg-success-light';
            } else {
                return 'bg-bg-hover';
            }
        },
        getStatusDotClass() {
            let baseClass = 'status-indicator-dot ';
            if (this.suite.failed > 0) {
                return baseClass + 'status-dot-failed';
            } else if (this.suite.passed > 0 && this.suite.passed + this.suite.skipped === this.suite.total) {
                return baseClass + 'status-dot-passed';
            } else if (this.suite.skipped > 0 && this.suite.skipped === this.suite.total) {
                return baseClass + 'status-dot-skipped';
            } else {
                return baseClass + 'status-dot-idle';
            }
        }
    }
};

// Test Operation Component
const TestOperation = {
    template: '#test-operation-template',
    components: {
        TestSuite
    },
    props: {
        operation: {
            type: Object,
            required: true
        }
    },
    data() {
        return {
            expanded: false
        };
    },
    computed: {
        sortedSuites() {
            return Object.values(this.operation.suites || {}).sort((a, b) =>
                a.name.localeCompare(b.name)
            );
        }
    },
    methods: {
        toggleExpanded() {
            this.expanded = !this.expanded;
        },
        getHeaderClass() {
            if (this.operation.status === 'completed') {
                return this.operation.failed > 0 ? 'bg-danger-light' : 'bg-success-light';
            } else if (this.operation.status === 'error') {
                return 'bg-warning-light';
            } else if (this.operation.status === 'running') {
                return 'bg-primary-light';
            }
            return '';
        },
        getStatusDotClass() {
            let baseClass = 'status-indicator-dot ';
            if (this.operation.status === 'completed' || this.operation.status === 'passed') {
                return baseClass + (this.operation.failed > 0 ? 'status-dot-failed' : 'status-dot-passed');
            } else if (this.operation.status === 'running') {
                return baseClass + 'status-dot-running animate-pulse-slow';
            } else if (this.operation.status === 'error' || this.operation.status === 'failed') {
                return baseClass + 'status-dot-error';
            } else if (this.operation.passed > 0 && this.operation.passed + this.operation.skipped === this.operation.total) {
                // If all tests are either passed or skipped, show as passed
                return baseClass + 'status-dot-passed';
            } else if (this.operation.failed > 0) {
                // If any tests failed
                return baseClass + 'status-dot-failed';
            } else if (this.operation.skipped > 0 && this.operation.skipped === this.operation.total) {
                // If all tests are skipped
                return baseClass + 'status-dot-skipped';
            }
            return baseClass + 'status-dot-idle';
        }
    }
};

// Initialize the Vue app when document is ready
document.addEventListener('DOMContentLoaded', () => {
    // Main App
    const app = Vue.createApp({
        template: '#app-template',
        components: {
            TestOperation,
            FlatTestList
        },
        data() {
            return {
                isLoading: false,
                loadingMessage: 'Loading tests...',
                operations: {},
                stats: {passed: 0, failed: 0, skipped: 0, total: 0, completed: 0},
                startTime: null,
                status: 'idle',
                globalError: null,
                testDataByCase: {},
                darkMode: false,
                viewMode: 'tree', // 'tree' or 'flat'

                // Sequential run tracking
                runQueue: [],
                isRunningSequentially: false,
                currentRunIndex: 0,

                // Modal state
                showLogModal: false,
                showErrorDetails: false,
                logModalTitle: '',
                logContent: ''
            };
        },
        computed: {
            statusMessage() {
                switch (this.status) {
                    case 'idle':
                        return 'Idle';
                    case 'running':
                        return `Running (${this.stats.completed}/${this.stats.total})`;
                    case 'passed':
                        return 'All Tests Passed';
                    case 'failed':
                        return 'Tests Failed';
                    case 'error':
                        return 'Error';
                    default:
                        return 'Unknown Status';
                }
            },
            statusDotClass() {
                let baseClass = 'status-indicator-dot ';
                switch (this.status) {
                    case 'idle':
                        return baseClass + 'status-dot-idle';
                    case 'running':
                        return baseClass + 'status-dot-running animate-pulse-slow';
                    case 'passed':
                        return baseClass + 'status-dot-passed';
                    case 'failed':
                        return baseClass + 'status-dot-failed';
                    case 'error':
                        return baseClass + 'status-dot-error';
                    default:
                        return baseClass + 'status-dot-idle';
                }
            },
            formattedDuration() {
                if (!this.startTime) return '-';
                const durationSec = (Date.now() - this.startTime) / 1000;
                return durationSec.toFixed(2) + 's';
            },
            sortedOperationGroups() {
                // Group operations by their group
                const groups = {};
                Object.values(this.operations).forEach(op => {
                    const group = op.group || 'default';
                    if (!groups[group]) {
                        groups[group] = [];
                    }
                    groups[group].push(op);
                });

                // Sort operations within each group
                Object.values(groups).forEach(ops => {
                    ops.sort((a, b) => a.name.localeCompare(b.name));
                });

                // Return sorted entries
                return Object.entries(groups).sort((a, b) => a[0].localeCompare(b[0]));
            },
            hasMultipleGroups() {
                // Check if there's more than one group
                return new Set(Object.values(this.operations).map(op => op.group || 'default')).size > 1;
            },
            flatTestItems() {
                // Create a flat list of all test items
                const items = [];

                Object.values(this.operations).forEach(op => {
                    Object.values(op.suites || {}).forEach(suite => {
                        (suite.tests || []).forEach(test => {
                            items.push({
                                opId: op.id,
                                opName: op.name,
                                group: op.group,
                                suite: suite.name,
                                test: test
                            });
                        });
                    });
                });

                // Sort by group, then suite, then test name
                return items.sort((a, b) => {
                    if (a.group !== b.group) return a.group.localeCompare(b.group);
                    if (a.suite !== b.suite) return a.suite.localeCompare(b.suite);
                    return a.test.name.localeCompare(b.test.name);
                });
            }
        },
        mounted() {
            this.initTheme();
            this.discoverTests();
        },
        methods: {
            // Theme Management
            initTheme() {
                // Check for saved theme preference or use system preference
                const savedTheme = localStorage.getItem('theme');
                if (savedTheme === 'dark') {
                    document.documentElement.classList.add('dark');
                    this.darkMode = true;
                } else if (savedTheme === 'light') {
                    document.documentElement.classList.remove('dark');
                    this.darkMode = false;
                } else {
                    // Use system preference
                    if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
                        document.documentElement.classList.add('dark');
                        this.darkMode = true;
                    }
                }

                // Check for saved view mode
                const savedViewMode = localStorage.getItem('viewMode');
                if (savedViewMode === 'flat' || savedViewMode === 'tree') {
                    this.viewMode = savedViewMode;
                }
            },
            toggleTheme() {
                this.darkMode = !this.darkMode;
                document.documentElement.classList.toggle('dark', this.darkMode);
                localStorage.setItem('theme', this.darkMode ? 'dark' : 'light');
            },
            toggleViewMode() {
                this.viewMode = this.viewMode === 'tree' ? 'flat' : 'tree';
                localStorage.setItem('viewMode', this.viewMode);
            },

            // Test Discovery
            discoverTests() {
                this.isLoading = true;
                this.loadingMessage = 'Discovering tests...';

                fetch('http://localhost:8082/api/v1/system/test/discover')
                    .then(response => {
                        if (!response.ok) {
                            throw new Error(`HTTP error! status: ${response.status}`);
                        }
                        return response.json();
                    })
                    .then(data => {
                        // Initialize operations from discovery data
                        (data.tests || []).forEach(op => {
                            this.operations[op.id] = {
                                id: op.id,
                                name: op.name,
                                group: op.group,
                                meta: op.meta,
                                status: 'pending',
                                suites: {},
                                passed: 0,
                                failed: 0,
                                skipped: 0,
                                total: 0
                            };
                        });

                        this.isLoading = false;
                    })
                    .catch(error => {
                        console.error('Error discovering tests:', error);
                        this.isLoading = false;
                        this.globalError = {
                            message: `Error discovering tests: ${error.message}`,
                            context: {
                                timestamp: new Date().toLocaleString(),
                                error: error.toString()
                            }
                        };
                    });
            },

            // Sequential Execution Management
            addToRunQueue(runItem) {
                this.runQueue.push(runItem);
                if (!this.isRunningSequentially) {
                    this.startSequentialRun();
                }
            },

            startSequentialRun() {
                if (this.runQueue.length === 0 || this.isRunningSequentially) return;

                this.isRunningSequentially = true;
                this.currentRunIndex = 0;
                this.executeNextInQueue();
            },

            executeNextInQueue() {
                if (this.currentRunIndex >= this.runQueue.length) {
                    this.isRunningSequentially = false;
                    this.runQueue = [];
                    return;
                }

                const runItem = this.runQueue[this.currentRunIndex];
                console.log(`Executing queue item ${this.currentRunIndex + 1}/${this.runQueue.length}:`, runItem);

                // Execute the appropriate test run based on type
                switch (runItem.type) {
                    case 'all':
                        this.executeRunAll();
                        break;
                    case 'specific':
                        this.executeRunSpecific(runItem.details);
                        break;
                    case 'suite':
                        this.executeRunSuite(runItem.details);
                        break;
                    case 'operation':
                        this.executeRunOperation(runItem.opId);
                        break;
                    case 'group':
                        this.executeRunGroup(runItem.groupName);
                        break;
                }
            },

            onTestRunComplete() {
                // This is called when a test run is completed
                this.currentRunIndex++;
                setTimeout(() => {
                    this.executeNextInQueue();
                }, 500); // Small delay before next run
            },

            // Test Execution
            runTests() {
                this.resetTestState();
                this.addToRunQueue({ type: 'all' });
            },

            executeRunAll() {
                this.resetTestState();
                this.isLoading = true;
                this.loadingMessage = 'Initializing tests...';
                this.startTime = Date.now();
                this.status = 'running';

                this.fetchTestResults('http://localhost:8082/api/v1/system/test');
            },

            runSpecificTest(details) {
                // Extract test details
                const {opId, suite, test} = details;

                // Add to run queue
                this.addToRunQueue({
                    type: 'specific',
                    details: details
                });
            },

            executeRunSpecific(details) {
                const {opId, suite, test} = details;
                const testName = typeof test === 'string' ? test : test.name;

                // Mark this specific test as running
                if (this.operations[opId] &&
                    this.operations[opId].suites[suite] &&
                    this.operations[opId].suites[suite].tests) {

                    const testObj = this.operations[opId].suites[suite].tests.find(t => t.name === testName);
                    if (testObj) {
                        testObj.status = 'running';
                    }
                }

                // Set the status to running
                this.status = 'running';
                this.startTime = Date.now();

                // Build the URL with all necessary parameters
                let url = `http://localhost:8082/api/v1/system/test/run?test_id=${encodeURIComponent(opId)}`;

                // Add suite and test parameters if available
                if (suite) {
                    url += `&suite=${encodeURIComponent(suite)}`;
                }

                if (testName) {
                    url += `&test=${encodeURIComponent(testName)}`;
                }

                console.log(`Running specific test with URL: ${url}`);
                this.fetchTestResults(url);
            },

            runSuiteTests(details) {
                this.addToRunQueue({
                    type: 'suite',
                    details: details
                });
            },

            executeRunSuite(details) {
                const {opId, suite} = details;

                // Mark all tests in this suite as running
                if (this.operations[opId] && this.operations[opId].suites[suite]) {
                    this.operations[opId].suites[suite].tests.forEach(test => {
                        test.status = 'running';
                    });
                }

                // Set the status to running
                this.status = 'running';
                this.startTime = Date.now();

                // Build the URL with test_id and suite parameters
                const url = `http://localhost:8082/api/v1/system/test/run?test_id=${encodeURIComponent(opId)}&suite=${encodeURIComponent(suite)}`;
                console.log(`Running suite tests with URL: ${url}`);
                this.fetchTestResults(url);
            },

            runOperationTests(opId) {
                this.addToRunQueue({
                    type: 'operation',
                    opId: opId
                });
            },

            executeRunOperation(opId) {
                // Mark all tests in this operation as running
                if (this.operations[opId]) {
                    Object.values(this.operations[opId].suites).forEach(suite => {
                        suite.tests.forEach(test => {
                            test.status = 'running';
                        });
                    });

                    // Also mark the operation as running
                    this.operations[opId].status = 'running';
                }

                // Set the status to running
                this.status = 'running';
                this.startTime = Date.now();

                // Build the URL with test_id parameter
                const url = `http://localhost:8082/api/v1/system/test/run?test_id=${encodeURIComponent(opId)}`;
                this.fetchTestResults(url);
            },

            runGroupTests(groupName) {
                this.addToRunQueue({
                    type: 'group',
                    groupName: groupName
                });
            },

            executeRunGroup(groupName) {
                // Mark all tests in this group as running
                Object.values(this.operations).forEach(op => {
                    if (op.group === groupName) {
                        Object.values(op.suites).forEach(suite => {
                            suite.tests.forEach(test => {
                                test.status = 'running';
                            });
                        });

                        // Also mark the operation as running
                        op.status = 'running';
                    }
                });

                // Set the status to running
                this.status = 'running';
                this.startTime = Date.now();

                // Build the URL with group parameter
                const url = `http://localhost:8082/api/v1/system/test/run?group=${encodeURIComponent(groupName)}`;
                this.fetchTestResults(url);
            },

            resetTestState() {
                this.globalError = null;
                this.testDataByCase = {};
                this.stats = {passed: 0, failed: 0, skipped: 0, total: 0, completed: 0};
                this.activeTestId = null;

                // Reset all test statuses to pending, but keep the structure
                Object.values(this.operations).forEach(op => {
                    op.status = 'pending';
                    op.passed = 0;
                    op.failed = 0;
                    op.skipped = 0;
                    op.total = 0;

                    Object.values(op.suites || {}).forEach(suite => {
                        suite.passed = 0;
                        suite.failed = 0;
                        suite.skipped = 0;
                        suite.total = 0;

                        (suite.tests || []).forEach(test => {
                            test.status = 'pending';
                            test.error = null;
                            test.duration = null;
                        });
                    });
                });
            },

            fetchTestResults(url) {
                fetch(url)
                    .then(response => {
                        if (!response.ok) {
                            throw new Error(`HTTP error! status: ${response.status}`);
                        }

                        const reader = response.body.getReader();
                        const decoder = new TextDecoder();

                        this.processStream(reader, decoder);
                    })
                    .catch(error => {
                        console.error('Error fetching test results:', error);
                        this.isLoading = false;
                        this.status = 'error';
                        this.globalError = {
                            message: `Error fetching test results: ${error.message}`,
                            context: {
                                timestamp: new Date().toLocaleString(),
                                error: error.toString()
                            }
                        };
                        // Notify that the run is complete even if there was an error
                        this.onTestRunComplete();
                    });
            },

            async processStream(reader, decoder) {
                try {
                    let buffer = ''; // Buffer for incomplete JSON strings

                    while (true) {
                        const {done, value} = await reader.read();

                        if (done) {
                            // Process any remaining data in the buffer
                            if (buffer.trim()) {
                                try {
                                    const data = JSON.parse(buffer);
                                    this.processTestEvent(data);
                                } catch (e) {
                                    console.error('Error parsing remaining JSON:', e, buffer);
                                }
                            }

                            this.finishTestRun();
                            this.onTestRunComplete();
                            break;
                        }

                        const chunk = decoder.decode(value, {stream: true});
                        buffer += chunk;

                        // Process complete JSON objects
                        const objects = this.extractJsonObjects(buffer);
                        buffer = objects.remainder;

                        for (const jsonStr of objects.complete) {
                            try {
                                const data = JSON.parse(jsonStr);
                                this.processTestEvent(data);
                            } catch (e) {
                                console.error('Error parsing JSON:', e, jsonStr);
                            }
                        }
                    }
                } catch (error) {
                    console.error('Error processing stream:', error);
                    this.status = 'error';
                    this.globalError = {
                        message: `Error processing test data: ${error.message}`,
                        context: {
                            timestamp: new Date().toLocaleString(),
                            error: error.toString()
                        }
                    };
                    this.onTestRunComplete();
                } finally {
                    this.isLoading = false;
                }
            },

            // More robust JSON extraction for streaming data
            extractJsonObjects(str) {
                const result = {
                    complete: [],
                    remainder: ''
                };

                let depth = 0;
                let startPos = -1;

                for (let i = 0; i < str.length; i++) {
                    const char = str[i];

                    if (char === '{') {
                        if (depth === 0) {
                            startPos = i;
                        }
                        depth++;
                    } else if (char === '}') {
                        depth--;
                        if (depth === 0 && startPos !== -1) {
                            // We have a complete JSON object
                            result.complete.push(str.substring(startPos, i + 1));
                            startPos = -1;
                        }
                    }
                }

                // Store the remainder
                if (startPos !== -1) {
                    result.remainder = str.substring(startPos);
                }

                return result;
            },

            // Event Processing
            processTestEvent(data) {
                // Determine if this is a new protocol or legacy format
                if (data.type) {
                    // New protocol format with "type" field
                    this.processNewProtocolEvent(data);
                } else {
                    // Legacy format with "event" field
                    this.processLegacyEvent(data);
                }
            },

            processNewProtocolEvent(data) {
                // Extract event type and data from the new protocol format
                const eventType = data.type;
                const eventData = data.data || {};

                // Use test_id from event data if available, otherwise use ref_id
                const testId = eventData.test_id || eventData.ref_id || eventData.id;

                if (testId) {
                    // Use this test_id for the current operation if needed
                    this.activeTestId = testId;
                }

                // Store event data by test case for detailed view
                if (testId && (eventType === 'test:case:start' ||
                    eventType === 'test:case:pass' ||
                    eventType === 'test:case:fail' ||
                    eventType === 'test:case:skip')) {

                    const caseId = `${testId}:${eventData.suite}:${eventData.test}`;
                    if (!this.testDataByCase[caseId]) {
                        this.testDataByCase[caseId] = [];
                    }

                    // Add formatted event to test case data
                    this.testDataByCase[caseId].push({
                        time: eventData.timestamp,
                        suite: eventData.suite,
                        test: eventData.test,
                        status: eventType.replace('test:case:', ''),
                        duration: eventData.duration,
                        error: eventData.error
                    });
                }

                // Handle different event types according to the new protocol
                switch (eventType) {
                    case 'test:discover':
                        this.handleTestDiscovery(eventData);
                        break;

                    case 'test:suite:start':
                        this.handleOperationStart(eventData);
                        break;

                    case 'test:plan':
                        this.handleTestPlan(eventData);
                        break;

                    case 'test:case:start':
                        this.handleTestCase({
                            ref_id: this.activeTestId,
                            suite: eventData.suite,
                            test: eventData.test,
                            status: 'running',
                            time: eventData.timestamp
                        });
                        break;

                    case 'test:case:pass':
                        this.handleTestCase({
                            ref_id: this.activeTestId,
                            suite: eventData.suite,
                            test: eventData.test,
                            status: 'passed',
                            duration: eventData.duration,
                            time: eventData.timestamp
                        });
                        break;

                    case 'test:case:fail':
                        this.handleTestCase({
                            ref_id: this.activeTestId,
                            suite: eventData.suite,
                            test: eventData.test,
                            status: 'failed',
                            duration: eventData.duration,
                            error: eventData.error,
                            time: eventData.timestamp
                        });
                        break;

                    case 'test:case:skip':
                        this.handleTestCase({
                            ref_id: this.activeTestId,
                            suite: eventData.suite,
                            test: eventData.test,
                            status: 'skipped',
                            reason: eventData.reason,
                            time: eventData.timestamp
                        });
                        break;

                    case 'test:complete':
                        // Update operation status with summary data
                        if (this.activeTestId && this.operations[this.activeTestId]) {
                            this.handleOperationEnd({
                                id: this.activeTestId,
                                status: eventData.status,
                                passed: eventData.passed,
                                failed: eventData.failed,
                                skipped: eventData.skipped,
                                total: eventData.total
                            });
                        }
                        break;

                    case 'test:summary':
                        this.updateGlobalStats(eventData);
                        break;

                    case 'test:fail':
                        // Handle test:fail event - this is the fix for the spinner issue
                        if (this.activeTestId && this.operations[this.activeTestId]) {
                            // Mark the operation as failed
                            this.operations[this.activeTestId].status = 'failed';

                            // Create an error message
                            const errorMessage = eventData.message || 'Test suite failed';

                            // Set global error for display
                            this.globalError = {
                                message: errorMessage,
                                context: {
                                    timestamp: new Date(eventData.timestamp * 1000).toLocaleString(),
                                    context: eventData.context,
                                    ...eventData
                                }
                            };

                            // Update global status
                            this.status = 'failed';
                        }
                        break;

                    case 'test:error':
                        this.globalError = {
                            message: eventData.message,
                            time: eventData.timestamp,
                            context: {
                                timestamp: new Date(eventData.timestamp * 1000).toLocaleString(),
                                context: eventData.context,
                                stack_trace: eventData.stack_trace,
                                ...eventData
                            }
                        };

                        // Mark running tests as errored
                        this.markRunningTestsAsErrored(eventData.message);
                        break;

                    case 'test:debug':
                        // Log debug messages but don't process them
                        console.log('Debug:', eventData);
                        break;
                }
            },

            processLegacyEvent(data) {
                // Process based on legacy event type
                switch (data.event) {
                    case 'test:discovery':
                        this.handleTestDiscovery(data);
                        break;
                    case 'test:operation:start':
                        this.handleOperationStart(data);
                        break;
                    case 'test:plan:client':
                        this.handleTestPlan(data);
                        break;
                    case 'test:case:client':
                        this.handleTestCase(data);
                        break;
                    case 'test:operation:end':
                        this.handleOperationEnd(data);
                        break;
                    case 'result':
                        if (data.data) {
                            this.updateOperationResult(data.test_id, data.data);
                        }
                        break;
                    case 'test:complete':
                        this.updateFinalSummary(data);
                        break;
                    case 'test:summary':
                        this.updateGlobalStats(data);
                        break;
                    case 'error':
                        this.globalError = {
                            message: data.message,
                            time: data.time,
                            context: {
                                timestamp: new Date(data.time * 1000).toLocaleString(),
                                operation: data.operation || 'Unknown',
                                ...data
                            }
                        };
                        this.markRunningTestsAsErrored(data.message);
                        break;
                }
            },

            // Event Handlers
            updateGlobalStats(data) {
                if (data.total !== undefined) {
                    this.stats.total = data.total;
                }
                if (data.completed !== undefined) {
                    this.stats.completed = data.completed;
                }
                if (data.failed !== undefined) {
                    this.stats.failed = data.failed;
                    if (data.failed > 0) {
                        this.status = 'failed';
                    }
                }
                if (data.passed !== undefined) {
                    this.stats.passed = data.passed;
                }
                if (data.skipped !== undefined) {
                    this.stats.skipped = data.skipped;
                }

                // Update overall status
                if (data.status === 'passed' && this.status !== 'failed') {
                    this.status = 'passed';
                } else if (data.status === 'failed') {
                    this.status = 'failed';
                }
            },

            handleTestDiscovery(data) {
                this.isLoading = false;

                // Initialize operations from discovery data
                (data.tests || []).forEach(op => {
                    if (!this.operations[op.id]) {
                        this.operations[op.id] = {
                            id: op.id,
                            name: op.name,
                            group: op.group,
                            meta: op.meta,
                            status: 'pending',
                            suites: {},
                            passed: 0,
                            failed: 0,
                            skipped: 0,
                            total: 0
                        };
                    }
                });
            },

            handleOperationStart(data) {
                const id = data.id || data.ref_id;
                const name = data.name;
                const group = data.group;

                // For new protocol compatibility
                this.activeTestId = id;

                // Don't create duplicate operations or reset existing ones
                if (this.operations[id]) {
                    // Just update status if already exists
                    this.operations[id].status = 'running';
                } else {
                    // Initialize a new operation
                    this.operations[id] = {
                        id: id,
                        name: name,
                        group: group || 'default',
                        status: 'running',
                        suites: {},
                        passed: 0,
                        failed: 0,
                        skipped: 0,
                        total: 0
                    };
                }
            },

            handleTestPlan(data) {
                const opId = data.test_id || data.ref_id || this.activeTestId;

                if (!this.operations[opId]) {
                    return;
                }

                // Reset operation counters for the new test plan
                this.operations[opId].passed = 0;
                this.operations[opId].failed = 0;
                this.operations[opId].skipped = 0;
                this.operations[opId].total = 0;

                // Initialize suites for this operation
                (data.suites || []).forEach(suite => {
                    const suiteName = suite.name;

                    // Create suite if it doesn't exist
                    if (!this.operations[opId].suites[suiteName]) {
                        this.operations[opId].suites[suiteName] = {
                            name: suiteName,
                            tests: [],
                            passed: 0,
                            failed: 0,
                            skipped: 0,
                            total: 0
                        };
                    }

                    // Reset suite counters
                    this.operations[opId].suites[suiteName].passed = 0;
                    this.operations[opId].suites[suiteName].failed = 0;
                    this.operations[opId].suites[suiteName].skipped = 0;
                    this.operations[opId].suites[suiteName].total = 0;

                    // Add or update tests in suite
                    suite.tests.forEach(planTest => {
                        let foundTest = this.operations[opId].suites[suiteName].tests.find(
                            t => t.name === planTest.name
                        );

                        if (foundTest) {
                            // Reset existing test
                            foundTest.status = planTest.skipped ? 'skipped' : 'pending';
                            foundTest.error = null;
                            foundTest.duration = null;

                            // Update skipped count if needed
                            if (planTest.skipped) {
                                this.operations[opId].suites[suiteName].skipped++;
                                this.operations[opId].skipped++;
                            }
                        } else {
                            // Add new test
                            this.operations[opId].suites[suiteName].tests.push({
                                name: planTest.name,
                                status: planTest.skipped ? 'skipped' : 'pending',
                                duration: null,
                                error: null
                            });

                            // Update skipped count if needed
                            if (planTest.skipped) {
                                this.operations[opId].suites[suiteName].skipped++;
                                this.operations[opId].skipped++;
                            }
                        }

                        // Update total count
                        this.operations[opId].suites[suiteName].total++;
                        this.operations[opId].total++;
                    });
                });

                // Only add to global total for new tests to avoid double counting
                this.stats.total = Object.values(this.operations).reduce(
                    (sum, op) => sum + op.total, 0
                );
                this.stats.skipped = Object.values(this.operations).reduce(
                    (sum, op) => sum + op.skipped, 0
                );
            },

            handleTestCase(data) {
                const testId = data.test_id || data.ref_id || this.activeTestId;
                const {suite, test, status, error, duration} = data;

                if (!testId || !this.operations[testId]) {
                    return;
                }

                // Create suite if it doesn't exist yet
                if (!this.operations[testId].suites[suite]) {
                    this.operations[testId].suites[suite] = {
                        name: suite,
                        tests: [],
                        passed: 0,
                        failed: 0,
                        skipped: 0,
                        total: 0
                    };
                }

                // Find the test in the suite
                let testItem = this.operations[testId].suites[suite].tests.find(t => t.name === test);

                // Create test if it doesn't exist yet
                if (!testItem) {
                    testItem = {
                        name: test,
                        status: 'pending',
                        duration: null,
                        error: null
                    };
                    this.operations[testId].suites[suite].tests.push(testItem);
                    this.operations[testId].suites[suite].total++;
                    this.operations[testId].total++;
                }

                // Store previous status for counter adjustment
                const prevStatus = testItem.status;

                // Update test status
                testItem.status = status;

                if (error !== undefined) {
                    testItem.error = error;
                }

                if (duration !== undefined) {
                    testItem.duration = duration;
                }

                // Only update counters if transitioning to a final state and not already in that state
                if (status === 'passed' && prevStatus !== 'passed') {
                    // Remove from other counters if needed
                    if (prevStatus === 'failed') {
                        this.operations[testId].suites[suite].failed--;
                        this.operations[testId].failed--;
                        this.stats.failed--;
                    } else if (prevStatus === 'skipped') {
                        this.operations[testId].suites[suite].skipped--;
                        this.operations[testId].skipped--;
                        this.stats.skipped--;
                    }

                    // Add to passed counter
                    this.operations[testId].suites[suite].passed++;
                    this.operations[testId].passed++;
                    this.stats.passed++;

                    // Only increment completed if not already in a final state
                    if (prevStatus !== 'failed' && prevStatus !== 'skipped') {
                        this.stats.completed++;
                    }
                }
                else if (status === 'failed' && prevStatus !== 'failed') {
                    // Remove from other counters if needed
                    if (prevStatus === 'passed') {
                        this.operations[testId].suites[suite].passed--;
                        this.operations[testId].passed--;
                        this.stats.passed--;
                    } else if (prevStatus === 'skipped') {
                        this.operations[testId].suites[suite].skipped--;
                        this.operations[testId].skipped--;
                        this.stats.skipped--;
                    }

                    // Add to failed counter
                    this.operations[testId].suites[suite].failed++;
                    this.operations[testId].failed++;
                    this.stats.failed++;
                    this.status = 'failed';

                    // Only increment completed if not already in a final state
                    if (prevStatus !== 'passed' && prevStatus !== 'skipped') {
                        this.stats.completed++;
                    }
                }
                else if (status === 'skipped' && prevStatus !== 'skipped') {
                    // Remove from other counters if needed
                    if (prevStatus === 'passed') {
                        this.operations[testId].suites[suite].passed--;
                        this.operations[testId].passed--;
                        this.stats.passed--;
                    } else if (prevStatus === 'failed') {
                        this.operations[testId].suites[suite].failed--;
                        this.operations[testId].failed--;
                        this.stats.failed--;
                    }

                    // Add to skipped counter
                    this.operations[testId].suites[suite].skipped++;
                    this.operations[testId].skipped++;
                    this.stats.skipped++;

                    // Only increment completed if not already in a final state
                    if (prevStatus !== 'passed' && prevStatus !== 'failed') {
                        this.stats.completed++;
                    }
                }

                // If final status achieved for all tests in suite, update suite status
                const suite_obj = this.operations[testId].suites[suite];
                if (suite_obj.passed + suite_obj.failed + suite_obj.skipped === suite_obj.total) {
                    if (suite_obj.failed > 0) {
                        // At least one test failed
                        suite_obj.status = 'failed';
                    } else {
                        // All tests passed or were skipped
                        suite_obj.status = 'passed';
                    }
                } else {
                    // Still tests running or pending
                    suite_obj.status = 'running';
                }
            },

            handleOperationEnd(data) {
                const {id, status, passed, failed, skipped, total} = data;
                const opId = id || this.activeTestId;

                if (!this.operations[opId]) {
                    return;
                }

                // Calculate final status
                let finalStatus = status || 'completed';
                if (failed > 0) {
                    finalStatus = 'failed';
                } else if ((passed + skipped) > 0 && (passed + skipped) === total) {
                    finalStatus = 'passed';
                }

                // Update operation with final status and metrics
                this.operations[opId].status = finalStatus;

                // Only update counters if provided and different
                if (passed !== undefined && this.operations[opId].passed !== passed) {
                    this.operations[opId].passed = passed;
                }

                if (failed !== undefined && this.operations[opId].failed !== failed) {
                    this.operations[opId].failed = failed;
                }

                if (skipped !== undefined && this.operations[opId].skipped !== skipped) {
                    this.operations[opId].skipped = skipped;
                }

                if (total !== undefined && this.operations[opId].total !== total) {
                    this.operations[opId].total = total;
                }

                // Update global status if any operation failed
                if (failed > 0 || finalStatus === 'failed') {
                    this.status = 'failed';
                }
            },

            updateOperationResult(opId, result) {
                if (!this.operations[opId]) return;

                // Update duration if available
                if (result.duration_ms) {
                    this.operations[opId].duration = result.duration_ms / 1000;
                }

                // Update operation status and counters from result
                if (result.failed_tests !== undefined) {
                    this.operations[opId].failed = result.failed_tests;
                    if (result.failed_tests > 0) {
                        this.operations[opId].status = 'failed';
                        this.status = 'failed';
                    }
                }

                if (result.passed_tests !== undefined) {
                    this.operations[opId].passed = result.passed_tests;
                }

                if (result.skipped_tests !== undefined) {
                    this.operations[opId].skipped = result.skipped_tests;
                }

                if (result.total_tests !== undefined) {
                    this.operations[opId].total = result.total_tests;
                }
            },

            updateFinalSummary(data) {
                const failedTests = data.tests_failed || data.failed || 0;
                const passedTests = data.tests_passed || data.passed || 0;
                const skippedTests = data.tests_skipped || data.skipped || 0;
                const totalTests = data.total || passedTests + failedTests + skippedTests || 0;

                // Always update status based on tests_failed, not just status field
                if (failedTests > 0) {
                    this.status = 'failed';
                } else if (this.status !== 'failed' && passedTests + skippedTests === totalTests) {
                    // Only set to passed if we don't already know it failed
                    // and all tests have concluded
                    this.status = 'passed';
                }
            },

            markRunningTestsAsErrored(errorMessage) {
                // For each operation
                Object.values(this.operations).forEach(operation => {
                    if (operation.status === 'running') {
                        operation.status = 'error';
                    }

                    // For each suite in the operation
                    Object.values(operation.suites).forEach(suite => {
                        // Check all running tests in this suite
                        suite.tests.forEach(test => {
                            if (test.status === 'running') {
                                // Only update counters if this is the first time marking as error
                                if (test.status !== 'error') {
                                    test.status = 'error';
                                    test.error = errorMessage;

                                    // Update counters
                                    suite.failed++;
                                    operation.failed++;
                                    this.stats.failed++;
                                    this.stats.completed++;
                                } else {
                                    // Just update status without changing counters
                                    test.status = 'error';
                                    test.error = errorMessage;
                                }

                                // Mark status as failed
                                this.status = 'failed';
                            }
                        });
                    });
                });
            },

            finishTestRun() {
                // Make sure status is correct based on pass/fail counts
                if (this.stats.failed > 0) {
                    this.status = 'failed';
                } else if (this.stats.total > 0 && this.stats.completed >= this.stats.total) {
                    this.status = 'passed';
                }

                // Mark any remaining "running" tests as errored
                Object.values(this.operations).forEach(operation => {
                    if (operation.status === 'running') {
                        operation.status = operation.failed > 0 ? 'failed' : 'completed';
                    }

                    Object.values(operation.suites).forEach(suite => {
                        suite.tests.forEach(test => {
                            if (test.status === 'running') {
                                // Only update if not already in error state
                                if (test.status !== 'error') {
                                    test.status = 'error';
                                    test.error = 'Test did not complete properly';

                                    // Update counters
                                    suite.failed++;
                                    operation.failed++;
                                    this.stats.failed++;
                                    this.stats.completed++;
                                    this.status = 'failed';
                                }
                            }
                        });
                    });
                });
            },

            // UI Helper Methods
            showTestDetails(details) {
                const {opId, suite, test} = details;
                let testName, testObj;

                if (typeof test === 'string') {
                    testName = test;
                } else {
                    testName = test.name;
                    testObj = test;
                }

                const testId = `${opId}:${suite}:${testName}`;
                const events = this.testDataByCase[testId] || [];

                this.logModalTitle = `${suite} - ${testName}`;

                // Format the log content
                let logText = '';
                if (events.length > 0) {
                    events.forEach(event => {
                        const timestamp = new Date(event.time * 1000).toLocaleTimeString();
                        let statusText = event.status;

                        if (statusText === 'passed') {
                            statusText = '✅ PASSED';
                        } else if (statusText === 'failed') {
                            statusText = '❌ FAILED';
                        } else if (statusText === 'running') {
                            statusText = '🔄 RUNNING';
                        } else if (statusText === 'error') {
                            statusText = '⚠️ ERROR';
                        } else if (statusText === 'skipped') {
                            statusText = '⏭️ SKIPPED';
                        }

                        logText += `[${timestamp}] ${statusText}\n`;

                        if (event.error) {
                            logText += `ERROR: ${event.error}\n`;
                        }

                        if (event.duration) {
                            logText += `Duration: ${event.duration.toFixed(3)}s\n`;
                        }

                        logText += '\n';
                    });
                } else if (testObj && testObj.error) {
                    // If we don't have event logs but we do have an error on the test object
                    logText = `ERROR: ${testObj.error}\n\n`;
                    if (testObj.duration) {
                        logText += `Duration: ${testObj.duration.toFixed(3)}s\n`;
                    }
                } else {
                    logText = 'No detailed log available for this test.';
                }

                this.logContent = logText;
                this.showLogModal = true;
            }
        }
    });

    // Register the components
    app.component('TestOperation', TestOperation);
    app.component('TestSuite', TestSuite);
    app.component('TestCase', TestCase);
    app.component('FlatTestList', FlatTestList);
    app.component('FlatTestItem', FlatTestItem);

    // Mount the app
    app.mount('#app');
});