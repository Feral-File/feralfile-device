import { FileInfo } from './r2';

export function generateHtml(files: FileInfo[]): string {
  // Group files by branch
  const groupedFiles = files.reduce((acc, file) => {
    if (!acc[file.branch]) {
      acc[file.branch] = [];
    }
    acc[file.branch].push(file);
    acc[file.branch].sort((a, b) => (b.lastUpdated || 0) - (a.lastUpdated || 0));
    return acc;
  }, {} as Record<string, FileInfo[]>);

  return `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Feral File Device Distribution</title>
    <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.3/dist/css/bootstrap.min.css" rel="stylesheet">
    <script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
    <style>
        .version-history {
            background: #f8f9fa;
        }
        .expand-btn {
            cursor: pointer;
            user-select: none;
        }
        .expand-btn:hover {
            color: #0d6efd;
        }
        .branch-label {
            white-space: nowrap;
        }
        .help-link {
            font-size: 0.8em;
            color: #6c757d;
            text-decoration: none;
            margin-left: 0.5rem;
        }
        .help-link:hover {
            color: #0d6efd;
            text-decoration: underline;
        }
        .release-notes {
            max-height: 70vh;
            overflow-y: auto;
        }
        .release-notes img {
            max-width: 100%;
            height: auto;
        }
        .share-btn {
            padding: 0.25rem 0.5rem;
            font-size: 0.875rem;
        }
        #shareUrl {
            font-family: monospace;
            font-size: 0.875rem;
        }
        tr {
            transition: opacity 0.3s ease-in-out, background-color 0.3s ease-in-out;
        }
    </style>
</head>
<body class="bg-light">
    <div class="container py-5">
        <div class="row mb-4">
            <div class="col">
                <h1 class="display-4">Feral File Device Distribution</h1>
            </div>
        </div>

        <div class="row">
            <div class="col">
                <div class="card shadow-sm">
                    <div class="card-body">
                        <div class="table-responsive">
                            <table class="table table-hover">
                                <thead>
                                    <tr>
                                        <th style="width: 1%"></th>
                                        <th>Branch</th>
                                        <th>Version</th>
                                        <th>Last Updated</th>
                                        <th>
                                            Image
                                            <a href="#" class="help-link" data-bs-toggle="modal" data-bs-target="#flashingModal">
                                                (How to flash?)
                                            </a>
                                        </th>
                                        <th>App Package</th>
                                        <th>Actions</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    ${Object.entries(groupedFiles).map(([branch, branchFiles]) => `
                                    <tr>
                                        <td class="expand-btn" data-branch="${branch}" onclick="toggleVersions('${branch}')">
                                            ${branchFiles.length > 1 ? '▶' : ''}
                                        </td>
                                        <td class="branch-label">
                                            ${branch}
                                            ${branch.startsWith('releases/') 
                                              ? '<span class="ms-2 badge bg-success">release</span>'
                                              : branch === 'main'
                                                ? '<span class="ms-2 badge bg-primary">stable</span>'
                                                : '<span class="ms-2 badge bg-warning text-dark">development</span>'
                                            }
                                            ${branchFiles[0].hasReleaseNotes ? `
                                            <a href="#" class="help-link ms-2" data-bs-toggle="modal" data-bs-target="#releaseNotesModal-${sanitizeId(branch)}-${sanitizeId(branchFiles[0].version)}">
                                                (What's new?)
                                            </a>
                                            ` : ''}
                                        </td>
                                        <td>
                                            ${branchFiles[0].version}
                                            ${branchFiles[0].tag === 'development' ? '<span class="ms-2 badge bg-danger">DEV</span>' : ''}
                                        </td>
                                        <td class="timestamp" data-timestamp="${branchFiles[0].lastUpdated || Date.now()}"></td>
                                        <td>
                                            ${branchFiles[0].zipUrl ? `
                                            <a href="/download/${branchFiles[0].zipUrl}" class="btn btn-sm btn-outline-primary">
                                                Download Image
                                                ${branchFiles[0].zipSize ? `<span class="ms-2 badge bg-secondary">${branchFiles[0].zipSize}</span>` : ''}
                                            </a>` : '-'}
                                        </td>
                                        <td>
                                            ${branchFiles[0].debUrl ? `
                                            <a href="/download/${branchFiles[0].debUrl}" class="btn btn-sm btn-outline-secondary">
                                                Download .deb
                                                ${branchFiles[0].debSize ? `<span class="ms-2 badge bg-secondary">${branchFiles[0].debSize}</span>` : ''}
                                            </a>` : '-'}
                                        </td>
                                        <td>
                                            <button class="btn btn-sm btn-outline-secondary share-btn" 
                                                    onclick="showShareDialog('${encodeURIComponent(branch)}', '${encodeURIComponent(branchFiles[0].version)}')">
                                                <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" class="bi bi-share" viewBox="0 0 16 16">
                                                    <path d="M13.5 1a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3M11 2.5a2.5 2.5 0 1 1 .603 1.628l-6.718 3.12a2.5 2.5 0 0 1 0 1.504l6.718 3.12a2.5 2.5 0 1 1-.488.876l-6.718-3.12a2.5 2.5 0 1 1 0-3.256l6.718-3.12A2.5 2.5 0 0 1 11 2.5m-8.5 4a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3m11 5.5a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3"/>
                                                </svg>
                                            </button>
                                        </td>
                                    </tr>
                                    ${branchFiles.slice(1).map(file => `
                                    <tr class="version-history d-none" data-branch="${branch}">
                                        <td></td>
                                        <td></td>
                                        <td>
                                            ${file.version}
                                            ${file.tag === 'development' ? '<span class="ms-2 badge bg-danger">DEV</span>' : ''}
                                        </td>
                                        <td class="timestamp" data-timestamp="${file.lastUpdated || Date.now()}"></td>
                                        <td>
                                            ${file.zipUrl ? `
                                            <a href="/download/${file.zipUrl}" class="btn btn-sm btn-outline-primary">
                                                Download Image
                                                ${file.zipSize ? `<span class="ms-2 badge bg-secondary">${file.zipSize}</span>` : ''}
                                            </a>` : '-'}
                                        </td>
                                        <td>
                                            ${file.debUrl ? `
                                            <a href="/download/${file.debUrl}" class="btn btn-sm btn-outline-secondary">
                                                Download .deb
                                                ${file.debSize ? `<span class="ms-2 badge bg-secondary">${file.debSize}</span>` : ''}
                                            </a>` : '-'}
                                        </td>
                                        <td>
                                            <button class="btn btn-sm btn-outline-secondary share-btn" 
                                                    onclick="showShareDialog('${encodeURIComponent(branch)}', '${encodeURIComponent(file.version)}')">
                                                <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" class="bi bi-share" viewBox="0 0 16 16">
                                                    <path d="M13.5 1a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3M11 2.5a2.5 2.5 0 1 1 .603 1.628l-6.718 3.12a2.5 2.5 0 0 1 0 1.504l6.718 3.12a2.5 2.5 0 1 1-.488.876l-6.718-3.12a2.5 2.5 0 1 1 0-3.256l6.718-3.12A2.5 2.5 0 0 1 11 2.5m-8.5 4a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3m11 5.5a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3"/>
                                                </svg>
                                            </button>
                                        </td>
                                    </tr>
                                    `).join('')}
                                    `).join('')}
                                </tbody>
                            </table>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <!-- Flashing Instructions Modal -->
    <div class="modal fade" id="flashingModal" tabindex="-1" aria-labelledby="flashingModalLabel" aria-hidden="true">
        <div class="modal-dialog modal-lg">
            <div class="modal-content">
                <div class="modal-header">
                    <h5 class="modal-title" id="flashingModalLabel">How to Flash the Image</h5>
                    <button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close"></button>
                </div>
                <div class="modal-body">
                    <h6>Steps to Flash Using BalenaEtcher:</h6>
                    <ol class="list-group list-group-numbered mb-3">
                        <li class="list-group-item">Download and install <a href="https://etcher.io/" target="_blank">BalenaEtcher</a></li>
                        <li class="list-group-item">Download the image file from this page</li>
                        <li class="list-group-item">Launch BalenaEtcher</li>
                        <li class="list-group-item">Click "Flash from file" and select the downloaded image</li>
                        <li class="list-group-item">Select your target SD card</li>
                        <li class="list-group-item">Click "Flash!" and wait for the process to complete</li>
                    </ol>
                    <div class="alert alert-info">
                        <strong>Default Credentials:</strong><br>
                        Username: feralfile<br>
                        Password: feralfile
                    </div>
                    <div class="alert alert-warning">
                        <strong>First Boot Instructions:</strong><br>
                        1. Attach a keyboard and mouse<br>
                        2. If no network connection is available:<br>
                        • Press Alt + F4 to exit kiosk mode<br>
                        • Use the GUI to configure Wi-Fi or connect Ethernet<br>
                        • Reboot to relaunch kiosk mode: <code>sudo reboot</code>
                    </div>
                </div>
                <div class="modal-footer">
                    <button type="button" class="btn btn-secondary" data-bs-dismiss="modal">Close</button>
                </div>
            </div>
        </div>
    </div>

    ${Object.entries(groupedFiles).map(([branch, branchFiles]) => 
      branchFiles[0].hasReleaseNotes ? `
      <div class="modal fade" id="releaseNotesModal-${sanitizeId(branch)}-${sanitizeId(branchFiles[0].version)}" 
           tabindex="-1" 
           aria-labelledby="releaseNotesModalLabel-${sanitizeId(branch)}" 
           aria-hidden="true">
          <div class="modal-dialog modal-lg">
              <div class="modal-content">
                  <div class="modal-header">
                      <h5 class="modal-title" id="releaseNotesModalLabel-${sanitizeId(branch)}">Release Notes - ${branchFiles[0].version}</h5>
                      <button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close"></button>
                  </div>
                  <div class="modal-body">
                      <div class="release-notes" 
                           data-branch="${branch}" 
                           data-version="${branchFiles[0].version}"
                           id="releaseNotes-${sanitizeId(branch)}-${sanitizeId(branchFiles[0].version)}">
                          <div class="text-center">
                              <div class="spinner-border" role="status">
                                  <span class="visually-hidden">Loading...</span>
                              </div>
                          </div>
                      </div>
                  </div>
                  <div class="modal-footer">
                      <button type="button" class="btn btn-secondary" data-bs-dismiss="modal">Close</button>
                  </div>
              </div>
          </div>
      </div>
      ` : ''
    ).join('')}

    <div class="modal fade" id="shareModal" tabindex="-1" aria-labelledby="shareModalLabel" aria-hidden="true">
        <div class="modal-dialog">
            <div class="modal-content">
                <div class="modal-header">
                    <h5 class="modal-title" id="shareModalLabel">Share Build</h5>
                    <button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close"></button>
                </div>
                <div class="modal-body">
                    <div class="mb-3">
                        <label for="shareUrl" class="form-label">Share URL:</label>
                        <div class="input-group">
                            <input type="text" class="form-control" id="shareUrl" readonly>
                            <button class="btn btn-outline-primary" onclick="copyShareUrl()">
                                Copy
                            </button>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <script src="https://cdn.jsdelivr.net/npm/bootstrap@5.3.3/dist/js/bootstrap.bundle.min.js"></script>
    <script>
    function toggleVersions(branch) {
        const btn = document.querySelector(\`.expand-btn[data-branch="\${branch}"]\`);
        const rows = document.querySelectorAll(\`.version-history[data-branch="\${branch}"]\`);
        const isExpanded = rows[0]?.classList.contains('d-none') === false;
        
        rows.forEach(row => row.classList.toggle('d-none'));
        btn.textContent = isExpanded ? '▶' : '▼';
    }

    // Format timestamps in local timezone
    function formatTimestamps() {
        const timestampElements = document.querySelectorAll('.timestamp');
        timestampElements.forEach(element => {
            const timestamp = parseInt(element.dataset.timestamp);
            if (!isNaN(timestamp)) {
                const date = new Date(timestamp);
                element.textContent = date.toLocaleString(undefined, {
                    year: 'numeric',
                    month: 'short',
                    day: 'numeric',
                    hour: '2-digit',
                    minute: '2-digit',
                    hour12: false,
                    timeZoneName: undefined
                });
            }
        });
    }

    // Format timestamps when page loads
    formatTimestamps();

    // Render markdown for release notes
    document.addEventListener('DOMContentLoaded', function() {
        const modals = document.querySelectorAll('.modal');
        modals.forEach(modal => {
            modal.addEventListener('show.bs.modal', async function() {
                const releaseNotesDiv = this.querySelector('.release-notes');
                if (releaseNotesDiv && !releaseNotesDiv.dataset.loaded) {
                    const branch = releaseNotesDiv.dataset.branch;
                    const version = releaseNotesDiv.dataset.version;
                    try {
                        const response = await fetch(\`/api/release-notes/\${encodeURIComponent(branch)}/\${encodeURIComponent(version)}\`);
                        if (!response.ok) throw new Error('Failed to load release notes');
                        const markdown = await response.text();
                        releaseNotesDiv.innerHTML = marked.parse(markdown);
                        releaseNotesDiv.dataset.loaded = 'true';
                    } catch (error) {
                        releaseNotesDiv.innerHTML = '<div class="alert alert-danger">Failed to load release notes</div>';
                    }
                }
            });
        });
    });

    function showShareDialog(branch, version) {
        const url = new URL(window.location.href);
        url.searchParams.set('branch', branch);
        url.searchParams.set('version', version);
        
        const shareUrl = document.getElementById('shareUrl');
        shareUrl.value = url.toString();
        
        const shareModal = new bootstrap.Modal(document.getElementById('shareModal'));
        shareModal.show();
    }

    function copyShareUrl() {
        const shareUrl = document.getElementById('shareUrl');
        shareUrl.select();
        document.execCommand('copy');
        
        const copyBtn = shareUrl.nextElementSibling;
        const originalText = copyBtn.textContent;
        copyBtn.textContent = 'Copied!';
        setTimeout(() => {
            copyBtn.textContent = originalText;
        }, 2000);
    }

    // Handle shared URLs on page load
    document.addEventListener('DOMContentLoaded', function() {
        const params = new URLSearchParams(window.location.search);
        const branch = params.get('branch');
        const version = params.get('version');
        
        if (branch && version) {
            const rows = document.querySelectorAll('tbody tr');
            
            // Function to flash the row
            const flashRow = (row) => {
                let flashCount = 0;
                const flash = () => {
                    if (flashCount >= 6) { // 3 complete cycles (on/off counts as 2)
                        row.style.backgroundColor = '#f8f9fa'; // Final gray color
                        return;
                    }
                    
                    row.style.backgroundColor = flashCount % 2 === 0 ? '#fff3cd' : 'transparent';
                    flashCount++;
                    setTimeout(flash, 300); // 300ms per state change
                };
                
                flash();
            };

            rows.forEach(row => {
                const branchCell = row.querySelector('.branch-label');
                const branchText = branchCell ? 
                    branchCell.childNodes[0].textContent.trim() : 
                    '';
                
                const versionCell = row.querySelector('td:nth-child(3)');
                const rowVersion = versionCell ? versionCell.textContent.trim() : '';
                
                if (decodeURIComponent(branch) === branchText && 
                    decodeURIComponent(version) === rowVersion) {
                    row.style.opacity = '1';
                    flashRow(row);
                    
                    const parentRow = row.classList.contains('version-history') ? 
                        row.previousElementSibling : 
                        row;
                    const branchBtn = parentRow.querySelector('.expand-btn');
                    if (branchBtn && branchBtn.textContent === '▶') {
                        toggleVersions(decodeURIComponent(branch));
                    }
                    
                    setTimeout(() => {
                        row.scrollIntoView({ behavior: 'smooth', block: 'center' });
                    }, 100);
                } else {
                    row.style.opacity = '0.5';
                    row.style.backgroundColor = '';
                }
            });

            // Check hidden version history rows
            const hiddenRows = document.querySelectorAll('.version-history.d-none');
            hiddenRows.forEach(row => {
                const rowBranch = row.getAttribute('data-branch');
                const versionCell = row.querySelector('td:nth-child(3)');
                const rowVersion = versionCell ? versionCell.textContent.trim() : '';
                
                if (decodeURIComponent(branch) === rowBranch && 
                    decodeURIComponent(version) === rowVersion) {
                    toggleVersions(decodeURIComponent(branch));
                    row.style.opacity = '1';
                    flashRow(row);
                    
                    setTimeout(() => {
                        row.scrollIntoView({ behavior: 'smooth', block: 'center' });
                    }, 100);
                }
            });
        }
    });
    </script>
</body>
</html>
`;
}

function sanitizeId(str: string): string {
  return str.replace(/[^a-zA-Z0-9]/g, '-');
} 