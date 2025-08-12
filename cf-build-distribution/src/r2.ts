interface FileInfo {
  branch: string;
  version: string;
  debUrl: string;
  zipUrl: string;
  debSize?: string;
  zipSize?: string;
  debEtag?: string;
  zipEtag?: string;
  lastUpdated?: number;  // timestamp in milliseconds
  hasReleaseNotes?: boolean; 
  tag: 'development' | 'production';
}

interface VersionInfo {
  latest_version: string;
  min_version: string;
  image_url: string;
  app_url: string;
  image_fingerprint?: string;
  app_fingerprint?: string;
}

function formatFileSize(bytes: number): string {
  const units = ['B', 'KB', 'MB', 'GB'];
  let size = bytes;
  let unitIndex = 0;
  
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex++;
  }
  
  return `${size.toFixed(1)} ${units[unitIndex]}`;
}

export async function listFiles(bucket: R2Bucket): Promise<FileInfo[]> {
  const objects = await bucket.list();
  const files: FileInfo[] = [];

  for (const obj of objects.objects) {
    const parts = obj.key.split('/');
    const filename = parts.pop();
    const branch = parts.join('/');
    if (!filename) continue;

    const newMatch = filename.match(/^FF-X1-(release|demo|develop|other)-(dev-)?(\d+\.\d+\.\d+)\.zip$/);
    const oldMatch = filename.match(/^radxa-x4-arch-(dev-)?(\d+\.\d+\.\d+)\.zip$/);
    
    if (newMatch) {
      const imageTag = newMatch[1]; // release, demo, develop, other
      const isDev = !!newMatch[2];
      const version = newMatch[3];

      files.push({
        branch,
        version,
        zipUrl: obj.key,
        zipSize: formatFileSize(obj.size),
        zipEtag: obj.etag?.replace(/['"]/g, ''),
        debUrl: '',
        debSize: undefined,
        debEtag: undefined,
        lastUpdated: obj.uploaded?.getTime(),
        tag: isDev ? 'development' : 'production',
      });
    } else if (oldMatch) {
      const isDev = !!oldMatch[1];
      const version = oldMatch[2];

      files.push({
        branch,
        version,
        zipUrl: obj.key,
        zipSize: formatFileSize(obj.size),
        zipEtag: obj.etag?.replace(/['"]/g, ''),
        debUrl: '',
        debSize: undefined,
        debEtag: undefined,
        lastUpdated: obj.uploaded?.getTime(),
        tag: isDev ? 'development' : 'production',
      });
    }
  }

  for (const obj of objects.objects) {
    const match = obj.key.match(/release_notes_(.+?)\.md$/);
    if (match) {
      const version = match[1];
      const branch = obj.key.split('/').slice(0, -1).join('/');
      const file = files.find(f => f.branch === branch && f.version === version);
      if (file) {
        file.hasReleaseNotes = true;
      }
    }
  }

  files.sort((a, b) => {
    if (a.branch === 'main') return -1;
    if (b.branch === 'main') return 1;

    const branchCompare = a.branch.localeCompare(b.branch);
    if (branchCompare !== 0) return branchCompare;

    const versionA = a.version.split('.').map(Number);
    const versionB = b.version.split('.').map(Number);

    for (let i = 0; i < 3; i++) {
      if (versionA[i] !== versionB[i]) {
        return versionB[i] - versionA[i];
      }
    }
    return 0;
  });

  return files;
}

export async function getLatestVersion(bucket: R2Bucket, branch: string): Promise<VersionInfo | null> {
  const files = await listFiles(bucket);
  
  // Filter files by branch and find the latest version
  const branchFiles = files.filter(f => f.branch === branch);
  if (branchFiles.length === 0) return null;

  // Sort by version (assuming semantic versioning)
  branchFiles.sort((a, b) => {
    const versionA = a.version.split('.').map(Number);
    const versionB = b.version.split('.').map(Number);
    
    for (let i = 0; i < 3; i++) {
      if (versionA[i] !== versionB[i]) {
        return versionB[i] - versionA[i];
      }
    }
    return 0;
  });

  const latest = branchFiles[0];
  return {
    latest_version: latest.version,
    min_version: latest.version,
    image_url: latest.zipUrl ? `/download/${latest.zipUrl}` : '',
    app_url: latest.debUrl ? `/download/${latest.debUrl}` : '',
    image_fingerprint: latest.zipEtag,
    app_fingerprint: latest.debEtag
  };
} 