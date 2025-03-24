/**
 * upload.js - Document upload functionality for Fortress Legal Portal
 */

// Global state
let api;
let config;
let currentFile = null;
let allDocuments = [];
let uploadInProgress = false;

// Initialize when DOM is fully loaded
document.addEventListener('DOMContentLoaded', () => {
    console.log('Upload module initializing...');
    initializeUploadModule();
});

/**
 * Initialize the upload module
 */
async function initializeUploadModule() {
    try {
        // Initialize app and get API and config
        const result = await init();
        api = result.api;
        config = result.config;
        console.log('App API is ready for upload module');

        // Set up event listeners
        setupEventListeners();

        // Fetch and display documents
        await fetchDocuments();

        // Show ready state
        updateStatusMessage('Upload module ready', 'info', 3000);
    } catch (err) {
        console.error('Failed to initialize upload module', err);
        document.getElementById('documentList').innerHTML = `
            <div class="px-4 py-5 text-center">
                <p class="text-red-500">Failed to initialize app: ${err.message}</p>
            </div>
        `;
        updateStatusMessage('Failed to initialize upload module: ' + err.message, 'error');
    }
}

/**
 * Set up all event listeners
 */
function setupEventListeners() {
    // File upload form submission
    const uploadForm = document.getElementById('uploadForm');
    if (uploadForm) {
        uploadForm.addEventListener('submit', handleFileUpload);
    }

    // Direct event handler for upload button as a fallback
    const uploadButton = document.getElementById('uploadButton');
    if (uploadButton) {
        uploadButton.addEventListener('click', function(e) {
            e.preventDefault();
            handleFileUpload(new Event('submit'));
        });
    }

    // File input change event to show selected file
    const fileInput = document.getElementById('fileInput');
    if (fileInput) {
        fileInput.addEventListener('change', (e) => {
            const fileName = e.target.files[0]?.name || 'No file selected';
            const fileSize = e.target.files[0]?.size || 0;
            const fileLabel = document.getElementById('selectedFileName');

            if (fileLabel) {
                fileLabel.textContent = fileName;
                fileLabel.classList.remove('text-gray-500');
                fileLabel.classList.add('text-gray-900');
            }

            const fileSizeLabel = document.getElementById('selectedFileSize');
            if (fileSizeLabel) {
                fileSizeLabel.textContent = formatFileSize(fileSize);
            }

            // Show the file info section
            const fileInfoSection = document.getElementById('fileInfoSection');
            if (fileInfoSection) {
                fileInfoSection.classList.remove('hidden');
            }
        });
    }

    // File drag and drop
    const dropArea = document.querySelector('.upload-drop-area');
    if (dropArea) {
        // Prevent default drag behaviors
        ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
            dropArea.addEventListener(eventName, preventDefaults, false);
        });

        // Highlight drop area when item is dragged over it
        ['dragenter', 'dragover'].forEach(eventName => {
            dropArea.addEventListener(eventName, () => {
                dropArea.classList.add('upload-drop-area-active');
            }, false);
        });

        // Remove highlight when item is dragged out or dropped
        ['dragleave', 'drop'].forEach(eventName => {
            dropArea.addEventListener(eventName, () => {
                dropArea.classList.remove('upload-drop-area-active');
            }, false);
        });

        // Handle dropped files
        dropArea.addEventListener('drop', (e) => {
            if (e.dataTransfer.files.length) {
                document.getElementById('fileInput').files = e.dataTransfer.files;

                // Trigger change event to update UI
                const changeEvent = new Event('change', { bubbles: true });
                document.getElementById('fileInput').dispatchEvent(changeEvent);
            }
        }, false);
    }

    // Document search
    const documentSearch = document.getElementById('documentSearch');
    if (documentSearch) {
        documentSearch.addEventListener('input', (e) => {
            const searchTerm = e.target.value.toLowerCase().trim();
            filterAndRenderDocuments(searchTerm);
        });
    }
}

/**
 * Prevent default behaviors for drag and drop events
 */
function preventDefaults(e) {
    e.preventDefault();
    e.stopPropagation();
}

/**
 * Handle file upload
 */
async function handleFileUpload(e) {
    e.preventDefault();

    if (uploadInProgress) {
        updateStatusMessage('Upload already in progress', 'warning');
        return;
    }

    const fileInput = document.getElementById('fileInput');
    if (!fileInput.files || fileInput.files.length === 0) {
        updateStatusMessage('Please select a file to upload', 'error');
        console.log('No file selected');
        return;
    }

    const file = fileInput.files[0];
    console.log(`Selected file: ${file.name} (${formatFileSize(file.size)})`);

    // Check file size (100MB limit)
    if (file.size > 100 * 1024 * 1024) {
        updateStatusMessage('File size exceeds 100MB limit', 'error');
        console.log('File size exceeds 100MB limit');
        return;
    }

    // Get auth token from different possible sources
    let authToken = getAuthToken();
    if (!authToken) {
        updateStatusMessage('Authentication failed - no token available', 'error');
        console.log('Authentication token is missing');
        return;
    }

    // Create FormData
    const formData = new FormData();
    formData.append('file', file);

    // Show upload status
    updateStatusMessage('Uploading file...', 'info');
    uploadInProgress = true;
    console.log('Upload started');

    try {
        updateUploadProgress(10);
        console.log(`Sending request to: http://localhost:8080/api/v1/files/upload`);

        // Upload file with fetch API
        const response = await fetch('http://localhost:8080/api/v1/files/upload', {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${authToken}`
            },
            body: formData
        });

        updateUploadProgress(70);
        console.log(`Server responded with status: ${response.status}`);

        if (!response.ok) {
            throw new Error(`Server returned error status: ${response.status}`);
        }

        const data = await response.json();

        updateUploadProgress(90);

        if (!data.success) {
            throw new Error(data.error || 'Upload failed');
        }

        console.log(`Upload successful! File ID: ${data.file.file_id}`);
        updateStatusMessage(`File "${file.name}" uploaded successfully!`, 'success');
        updateUploadProgress(100);

        // Reset form
        document.getElementById('uploadForm').reset();

        // Hide file info section
        const fileInfoSection = document.getElementById('fileInfoSection');
        if (fileInfoSection) {
            fileInfoSection.classList.add('hidden');
        }

        // Refresh document list
        setTimeout(() => {
            fetchDocuments();
        }, 1000);

    } catch (error) {
        console.error('Upload error:', error);
        console.log(`Error during upload: ${error.message}`);
        updateStatusMessage(`Upload failed: ${error.message}`, 'error');
    } finally {
        uploadInProgress = false;
    }
}

/**
 * Update the upload progress indicator
 */
function updateUploadProgress(percent) {
    const progressBar = document.getElementById('uploadProgressBar');
    if (progressBar) {
        progressBar.style.width = `${percent}%`;

        // Hide progress when complete
        if (percent >= 100) {
            setTimeout(() => {
                const progressContainer = document.getElementById('uploadProgressContainer');
                if (progressContainer) {
                    progressContainer.classList.add('hidden');
                }
                progressBar.style.width = '0%';
            }, 2000);
        } else {
            // Make sure it's visible
            const progressContainer = document.getElementById('uploadProgressContainer');
            if (progressContainer) {
                progressContainer.classList.remove('hidden');
            }
        }
    }
}

/**
 * Show upload status message
 */
function updateStatusMessage(message, type = 'info', autoDismiss = 0) {
    const statusElement = document.getElementById('uploadStatus');
    if (!statusElement) return;

    statusElement.textContent = message;
    statusElement.className = 'mt-2 text-sm'; // Reset classes

    switch (type) {
        case 'success':
            statusElement.classList.add('text-green-600');
            break;
        case 'error':
            statusElement.classList.add('text-red-600');
            break;
        case 'warning':
            statusElement.classList.add('text-yellow-600');
            break;
        default:
            statusElement.classList.add('text-gray-600');
    }

    statusElement.classList.remove('hidden');

    // Auto-dismiss if specified
    if (autoDismiss > 0) {
        setTimeout(() => {
            statusElement.classList.add('hidden');
        }, autoDismiss);
    }
}

/**
 * Format file size for display
 */
function formatFileSize(bytes) {
    if (!bytes) return '0 Bytes';

    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));

    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

/**
 * Fetch documents from the API
 */
async function fetchDocuments() {
    try {
        console.log("Fetching document list");

        // Show loading state
        document.getElementById('documentList').innerHTML = `
            <div class="px-4 py-3 text-center text-sm text-gray-500">
                Loading documents...
            </div>
        `;

        // Get auth token
        const authToken = getAuthToken();
        if (!authToken) {
            throw new Error('Authentication token is missing');
        }

        const response = await fetch('http://localhost:8080/api/v1/files', {
            headers: {
                'Authorization': `Bearer ${authToken}`,
                'Content-Type': 'application/json'
            }
        });

        if (!response.ok) {
            throw new Error(`HTTP error! Status: ${response.status}`);
        }

        const data = await response.json();
        console.log(`Received ${data.files?.length || 0} documents`);

        if (!data.success) {
            throw new Error(data.error || 'Failed to fetch documents');
        }

        // Store all documents for search functionality
        allDocuments = data.files || [];

        // Render document list
        renderDocumentList(allDocuments);

    } catch (error) {
        console.error('Error fetching documents:', error);
        console.log(`Error fetching documents: ${error.message}`);
        document.getElementById('documentList').innerHTML = `
            <div class="px-4 py-5 text-center">
                <p class="text-red-500">Failed to load documents: ${error.message}</p>
            </div>
        `;
    }
}

/**
 * Filter and render documents based on search term
 */
function filterAndRenderDocuments(searchTerm) {
    if (!searchTerm) {
        renderDocumentList(allDocuments);
        return;
    }

    // Filter documents by search term
    const filtered = allDocuments.filter(doc => {
        return doc.filename.toLowerCase().includes(searchTerm);
    });

    renderDocumentList(filtered);
}

/**
 * Render document list
 */
function renderDocumentList(documents) {
    const documentList = document.getElementById('documentList');
    if (!documentList) return;

    documentList.innerHTML = '';

    if (!documents || documents.length === 0) {
        documentList.innerHTML = `
            <div class="px-4 py-5 text-center">
                <p class="text-gray-500">No documents found</p>
            </div>
        `;
        return;
    }

    // Create document list table
    const table = document.createElement('table');
    table.className = 'min-w-full divide-y divide-gray-200 document-table';

    // Create table header
    const thead = document.createElement('thead');
    thead.className = 'bg-gray-50';
    thead.innerHTML = `
        <tr>
            <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                Document
            </th>
            <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                Status
            </th>
            <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                Size
            </th>
            <th scope="col" class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                Date
            </th>
            <th scope="col" class="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">
                Actions
            </th>
        </tr>
    `;

    // Create table body
    const tbody = document.createElement('tbody');
    tbody.className = 'bg-white divide-y divide-gray-200';

    // Add document rows
    documents.forEach((doc, index) => {
        const tr = document.createElement('tr');
        tr.className = index % 2 === 0 ? 'bg-white' : 'bg-gray-50';

        // Format date
        let documentDate;
        try {
            documentDate = new Date(doc.created_at || Date.now());
            if (isNaN(documentDate.getTime())) {
                documentDate = new Date();
            }
        } catch (e) {
            documentDate = new Date();
        }

        const formattedDate = documentDate.toLocaleDateString() + ' ' +
            documentDate.toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'});

        // Format file size
        const formattedSize = formatFileSize(doc.size);

        // Status badge color
        let statusColor = 'bg-gray-100 text-gray-800'; // default
        if (doc.status === 'ready') {
            statusColor = 'bg-green-100 text-green-800';
        } else if (doc.status === 'processing') {
            statusColor = 'bg-yellow-100 text-yellow-800';
        } else if (doc.status === 'error') {
            statusColor = 'bg-red-100 text-red-800';
        }

        tr.innerHTML = `
            <td class="px-6 py-4 file-name-cell">
                <div class="flex items-center">
                    <div class="flex-shrink-0 h-10 w-10 flex items-center justify-center">
                        <i class="fas fa-file-alt text-indigo-500 text-xl"></i>
                    </div>
                    <div class="ml-4 overflow-hidden">
                        <div class="text-sm font-medium text-gray-900 truncate file-name" title="${doc.filename}">${doc.filename}</div>
                        <div class="text-sm text-gray-500 truncate">${doc.mime_type || 'Unknown type'}</div>
                    </div>
                </div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
                <span class="px-2 inline-flex text-xs leading-5 font-semibold rounded-full ${statusColor}">
                    ${doc.status}
                </span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                ${formattedSize}
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                ${formattedDate}
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                <button data-file-id="${doc.file_id}" class="text-indigo-600 hover:text-indigo-900 view-details-btn">
                    View
                </button>
                <button data-file-id="${doc.file_id}" class="ml-3 text-red-600 hover:text-red-900 delete-file-btn">
                    Delete
                </button>
            </td>
        `;

        tbody.appendChild(tr);
    });

    table.appendChild(thead);
    table.appendChild(tbody);
    documentList.appendChild(table);

    // Add event listeners to buttons
    document.querySelectorAll('.view-details-btn').forEach(button => {
        button.addEventListener('click', () => {
            const fileId = button.getAttribute('data-file-id');
            viewDocumentDetails(fileId);
        });
    });

    document.querySelectorAll('.delete-file-btn').forEach(button => {
        button.addEventListener('click', () => {
            const fileId = button.getAttribute('data-file-id');
            deleteDocument(fileId);
        });
    });
}

/**
 * View document details
 */
async function viewDocumentDetails(fileId) {
    try {
        // Show loading state
        const detailSection = document.getElementById('documentDetailSection');
        if (detailSection) {
            detailSection.classList.remove('hidden');
        }

        const detailTitle = document.getElementById('detailTitle');
        if (detailTitle) {
            detailTitle.textContent = 'Document Details';
            detailTitle.className = "text-lg leading-6 font-medium text-gray-900";
        }

        const detailSubtitle = document.getElementById('detailSubtitle');
        if (detailSubtitle) {
            detailSubtitle.textContent = 'Loading document information...';
        }

        const documentDetail = document.getElementById('documentDetail');
        if (documentDetail) {
            documentDetail.innerHTML = `
                <div class="text-center py-4">
                    <i class="fas fa-spinner fa-spin text-indigo-500 text-2xl"></i>
                </div>
            `;
        }

        // Get auth token
        const authToken = getAuthToken();
        if (!authToken) {
            throw new Error('Authentication token is missing');
        }

        // Use query parameter
        const response = await fetch(`http://localhost:8080/api/v1/files/get?file_id=${fileId}`, {
            headers: {
                'Authorization': `Bearer ${authToken}`,
                'Content-Type': 'application/json'
            }
        });

        if (!response.ok) {
            throw new Error(`HTTP error! Status: ${response.status}`);
        }

        const data = await response.json();

        if (!data.success) {
            throw new Error(data.error || 'Failed to fetch document details');
        }

        // Store current file
        currentFile = data.file;

        // Update document details section
        if (detailTitle) {
            detailTitle.textContent = data.file.filename;
            detailTitle.title = data.file.filename; // Add title for tooltip on hover
            detailTitle.className = "text-lg leading-6 font-medium text-gray-900 truncate max-w-full";
        }

        if (detailSubtitle) {
            detailSubtitle.textContent = `File uploaded on ${new Date(data.file.created_at).toLocaleString()}`;
        }

        // Render document details with markdown content if available
        if (data.file.status === 'ready' && data.content) {
            renderDocumentWithContent(data.file, data.content);
        } else {
            renderDocumentBasicDetails(data.file);
        }

    } catch (error) {
        console.error('Error fetching document details:', error);

        const detailTitle = document.getElementById('detailTitle');
        if (detailTitle) {
            detailTitle.textContent = 'Error';
        }

        const detailSubtitle = document.getElementById('detailSubtitle');
        if (detailSubtitle) {
            detailSubtitle.textContent = 'Failed to load document details';
        }

        const documentDetail = document.getElementById('documentDetail');
        if (documentDetail) {
            documentDetail.innerHTML = `
                <div class="bg-red-50 border-l-4 border-red-400 p-4">
                    <div class="flex">
                        <div class="flex-shrink-0">
                            <i class="fas fa-exclamation-triangle text-red-400"></i>
                        </div>
                        <div class="ml-3">
                            <p class="text-sm text-red-700">
                                ${error.message}
                            </p>
                        </div>
                    </div>
                </div>
            `;
        }
    }
}

/**
 * Render document basic details
 */
function renderDocumentBasicDetails(file) {
    const detail = document.getElementById('documentDetail');
    if (!detail) return;

    // Format date
    const createdDate = new Date(file.created_at).toLocaleString();
    const updatedDate = new Date(file.updated_at).toLocaleString();

    detail.innerHTML = `
        <div class="space-y-4">
            <div class="bg-gray-50 px-4 py-5 sm:grid sm:grid-cols-3 sm:gap-4 sm:px-6">
                <dt class="text-sm font-medium text-gray-500">File name</dt>
                <dd class="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-2 break-words">${file.filename}</dd>
            </div>
            <div class="bg-white px-4 py-5 sm:grid sm:grid-cols-3 sm:gap-4 sm:px-6">
                <dt class="text-sm font-medium text-gray-500">Type</dt>
                <dd class="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-2">${file.mime_type}</dd>
            </div>
            <div class="bg-gray-50 px-4 py-5 sm:grid sm:grid-cols-3 sm:gap-4 sm:px-6">
                <dt class="text-sm font-medium text-gray-500">Size</dt>
                <dd class="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-2">${formatFileSize(file.size)}</dd>
            </div>
            <div class="bg-white px-4 py-5 sm:grid sm:grid-cols-3 sm:gap-4 sm:px-6">
                <dt class="text-sm font-medium text-gray-500">Status</dt>
                <dd class="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-2">
                    <span class="px-2 inline-flex text-xs leading-5 font-semibold rounded-full ${
        file.status === 'ready' ? 'bg-green-100 text-green-800' :
            file.status === 'processing' ? 'bg-yellow-100 text-yellow-800' :
                file.status === 'error' ? 'bg-red-100 text-red-800' :
                    'bg-gray-100 text-gray-800'
    }">
                        ${file.status}
                    </span>
                </dd>
            </div>
            <div class="bg-gray-50 px-4 py-5 sm:grid sm:grid-cols-3 sm:gap-4 sm:px-6">
                <dt class="text-sm font-medium text-gray-500">Created</dt>
                <dd class="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-2">${createdDate}</dd>
            </div>
            <div class="bg-white px-4 py-5 sm:grid sm:grid-cols-3 sm:gap-4 sm:px-6">
                <dt class="text-sm font-medium text-gray-500">Last updated</dt>
                <dd class="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-2">${updatedDate}</dd>
            </div>
        </div>
    `;

    // Show message for processing documents
    if (file.status === 'processing') {
        detail.innerHTML += `
            <div class="mt-4 bg-yellow-50 border-l-4 border-yellow-400 p-4">
                <div class="flex">
                    <div class="flex-shrink-0">
                        <i class="fas fa-exclamation-circle text-yellow-400"></i>
                    </div>
                    <div class="ml-3">
                        <p class="text-sm text-yellow-700">
                            This document is still being processed. Please check back later.
                        </p>
                    </div>
                </div>
            </div>
        `;
    } else if (file.status === 'error') {
        detail.innerHTML += `
            <div class="mt-4 bg-red-50 border-l-4 border-red-400 p-4">
                <div class="flex">
                    <div class="flex-shrink-0">
                        <i class="fas fa-exclamation-triangle text-red-400"></i>
                    </div>
                    <div class="ml-3">
                        <p class="text-sm text-red-700">
                            An error occurred while processing this document.
                        </p>
                    </div>
                </div>
            </div>
        `;
    }
}

/**
 * Render document with content
 */
function renderDocumentWithContent(file, content) {
    const detail = document.getElementById('documentDetail');
    if (!detail) return;

    // Add a dynamic script to load the Marked.js library
    if (!window.marked) {
        const script = document.createElement('script');
        script.src = 'https://cdnjs.cloudflare.com/ajax/libs/marked/4.3.0/marked.min.js';
        script.onload = function() {
            // Once Marked is loaded, render the content
            renderMarkdownContent(detail, file, content);
        };
        document.head.appendChild(script);
    } else {
        // If Marked is already loaded, render directly
        renderMarkdownContent(detail, file, content);
    }
}

/**
 * Render markdown content with Marked
 */
function renderMarkdownContent(detail, file, content) {
    // First add file metadata
    const createdDate = new Date(file.created_at).toLocaleString();
    const updatedDate = new Date(file.updated_at).toLocaleString();

    // Create metadata section
    const metadataHtml = `
        <div class="mb-6 bg-gray-50 p-4 rounded-md">
            <div class="flex justify-between items-center mb-2">
                <div class="truncate max-w-[70%]" title="${file.filename}">
                    <span class="px-2 inline-flex text-xs leading-5 font-semibold rounded-full ${
        file.status === 'ready' ? 'bg-green-100 text-green-800' :
            'bg-gray-100 text-gray-800'
    }">
                        ${file.status}
                    </span>
                    <span class="ml-2 text-sm text-gray-500">${formatFileSize(file.size)}</span>
                </div>
                <div class="text-sm text-gray-500">
                    Created: ${createdDate}
                </div>
            </div>
            <div class="flex justify-between items-center">
                <div class="text-sm text-gray-500 truncate max-w-[70%]">
                    Type: ${file.mime_type}
                </div>
                <div class="text-sm text-gray-500">
                    Last updated: ${updatedDate}
                </div>
            </div>
        </div>
    `;

    // Then render markdown content
    let renderedContent;
    try {
        renderedContent = marked.parse(content);
    } catch (e) {
        console.error('Failed to parse markdown:', e);
        renderedContent = `<pre class="whitespace-pre-wrap">${content}</pre>`;
    }

    detail.innerHTML = `
        ${metadataHtml}
        <div class="border-t border-gray-200 pt-4">
            <h4 class="text-lg font-medium text-gray-900 mb-4">Document Content</h4>
            <div class="prose prose-indigo max-w-none">
                ${renderedContent}
            </div>
        </div>
    `;
}

/**
 * Delete document
 */
async function deleteDocument(fileId) {
    if (!confirm('Are you sure you want to delete this document? This action cannot be undone.')) {
        return;
    }

    try {
        console.log(`Deleting document ${fileId}`);

        // Get auth token
        const authToken = getAuthToken();
        if (!authToken) {
            throw new Error('Authentication token is missing');
        }

        // DELETE request with the file_id as a query parameter
        const url = `http://localhost:8080/api/v1/files?file_id=${fileId}`;
        console.log(`Sending DELETE request to: ${url}`);

        const response = await fetch(url, {
            method: 'DELETE',
            headers: {
                'Authorization': `Bearer ${authToken}`,
                'Content-Type': 'application/json'
            }
        });

        if (!response.ok) {
            const errorText = await response.text();
            console.error('Server error response:', errorText);
            throw new Error(`HTTP error! Status: ${response.status}`);
        }

        const data = await response.json();
        console.log('Delete response:', data);

        if (!data.success) {
            throw new Error(data.error || 'Delete failed');
        }

        console.log(`Document deleted successfully`);

        // Hide detail section if this was the current file
        if (currentFile && currentFile.file_id === fileId) {
            const detailSection = document.getElementById('documentDetailSection');
            if (detailSection) {
                detailSection.classList.add('hidden');
            }
            currentFile = null;
        }

        // Show success message
        updateStatusMessage('Document deleted successfully', 'success', 3000);

        // Refresh document list
        fetchDocuments();

    } catch (error) {
        console.error('Delete error:', error);
        console.log(`Error deleting document: ${error.message}`);
        updateStatusMessage(`Failed to delete document: ${error.message}`, 'error');
    }
}

/**
 * Helper function to get auth token from various sources
 */
function getAuthToken() {
    let authToken = null;

    // Try to get from config
    if (config && config.auth && config.auth.token) {
        authToken = config.auth.token;
    }
    // Try to get from window.appConfig
    else if (window.appConfig && window.appConfig.auth && window.appConfig.auth.token) {
        authToken = window.appConfig.auth.token;
    }
    // Try to get from localStorage
    else {
        const storedToken = localStorage.getItem('auth_token');
        if (storedToken) {
            authToken = storedToken;
        }
    }

    return authToken;
}

// Export functions that might be needed externally
window.documentUpload = {
    fetchDocuments,
    handleFileUpload,
    updateStatusMessage,
    viewDocumentDetails,
    deleteDocument
};

// Log that script has loaded successfully
console.log('upload.js script loaded successfully');