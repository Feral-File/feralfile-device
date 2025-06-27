import { handleAuth } from './auth';
import { listFiles, getLatestVersion } from './r2';
import { generateHtml } from './html';

export interface Env {
  BUCKET: R2Bucket;
  AUTH_USERNAME: string;
  AUTH_PASSWORD: string;
}

export default {
  async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
    // Handle authentication
    const authResponse = await handleAuth(request, env);
    if (authResponse) return authResponse;

    const url = new URL(request.url);
    
    // Handle API request
    if (url.pathname.startsWith('/api/latest/')) {
      const branch = url.pathname.replace('/api/latest/', '');
      const versionInfo = await getLatestVersion(env.BUCKET, branch);
      
      if (!versionInfo) {
        return new Response(JSON.stringify({ error: 'Branch not found' }), {
          status: 404,
          headers: { 'Content-Type': 'application/json' }
        });
      }

      return new Response(JSON.stringify(versionInfo), {
        headers: { 'Content-Type': 'application/json' }
      });
    }

    // Handle file downloads
    if (url.pathname.startsWith('/download/')) {
      const key = url.pathname.replace('/download/', '');
      const object = await env.BUCKET.get(key);
      
      if (!object) {
        return new Response('File not found', { status: 404 });
      }

      return new Response(object.body, {
        headers: {
          'Content-Type': object.httpMetadata?.contentType || 'application/octet-stream',
          'Content-Length': object.size.toString(),
          'Content-Disposition': `attachment; filename="${key.split('/').pop()}"`,
        },
      });
    }

    if (url.pathname.startsWith('/pool/')) {
      const key = url.pathname.replace('/pool/', '');
      const object = await env.BUCKET.get(key);
      
      if (!object) {
        console.error(`File not found for key: ${key}`);
        return new Response(`File not found: ${url.pathname}`, { status: 404 });
      }

      return new Response(object.body, {
        headers: {
          'Content-Type': 'application/x-debian-package',
          'Content-Length': object.size.toString(),
          'Cache-Control': 'max-age=3600, public',
        },
      });
    }

    // Handle release notes
    if (url.pathname.startsWith('/api/release-notes/')) {
      const fullPath = decodeURIComponent(url.pathname.replace('/api/release-notes/', ''));
      const match = fullPath.match(/(.+)\/(.+)$/);  // Split at the last slash
      if (!match) {
        return new Response('Invalid release notes path', { status: 400 });
      }
      
      const [_, branch, version] = match;
      const key = `${branch}/release_notes_${version}.md`;
      
      console.log('Looking for release notes at:', key); // For debugging
      
      const object = await env.BUCKET.get(key);
      
      if (!object) {
        return new Response('Release notes not found', { status: 404 });
      }

      return new Response(await object.text(), {
        headers: { 'Content-Type': 'text/markdown' }
      });
    }

    if (url.pathname.startsWith('/dists/')) {
      const path = url.pathname.replace('/dists/', '');

      // Release / InRelease 
      // URL: /dists/<dist(branch)>/{Release|InRelease}
      if (path.endsWith('/Release') || path.endsWith('/InRelease')) {
        // Serve the Release file
        const key = path
        const object = await env.BUCKET.get(key);
        if (!object) {
          return new Response(`${key} not found`, { status: 404 });
        }
        
        return new Response(object.body, {
          headers: {
            'Content-Type': 'text/plain',
            'Content-Length': object.size.toString(),
            'Cache-Control': 'max-age=3600, public',
          },
        });
      }
    
      // Packages(.gz)
      // URL: /dists/<dist(branch)>/<component>/binary-{amd64|arm64}/{Packages|Packages.gz}
      if (path.endsWith('Packages') || path.endsWith('Packages.gz')) {
        const parts = path.split('/');
        const len = parts.length;

        const filename      = parts[len - 1];  // 'Packages' or 'Packages.gz'
        const archComponent = parts[len - 2];  // 'binary-arm64' or 'binary-amd64'
        const component     = parts[len - 3];  // e.g. 'main'
        const branch = parts.slice(0, len - 3).join('/'); // e.g. 'features/any-feature'
    
        const allowedComponents = ['main'];
        if (!allowedComponents.includes(component)) {
          console.error(`Unknown component: ${component}`);
          return new Response('Invalid component', { status: 400 });
        }

        // check arch
        const m = archComponent.match(/^binary-(amd64|arm64)$/);
        if (!m) {
          console.error(`Invalid arch path: ${archComponent}`);
          return new Response('Invalid arch', { status: 400 });
        }
        const arch = m[1]; // "amd64" or "arm64"
        const key = `${branch}/${arch}/${filename}`;
    
        console.log(`Fetching APT index from key: ${key}`);
        let object = await env.BUCKET.get(key);
        // FIXME: fallback to old release file to support old version, 
        // remove this once main branch build the next version 0.4.3
        if (!object && arch === 'arm64') {
          const fallbackKey = `${branch}/${filename}`;
          console.log(`Fetching old version APT index from key: ${key}`);
          object = await env.BUCKET.get(fallbackKey);
        }
        if (!object) {
          console.error(`Not found: ${key}`);
          return new Response(`${key} not found`, { status: 404 });
        }

        const isGz = filename === 'Packages.gz';
        const headers: HeadersInit = {
          'Content-Type': isGz ? 'application/gzip' : 'text/plain',
          'Content-Length': object.size.toString(),
          'Cache-Control': 'public, max-age=3600',
        };
        if (isGz) headers['Content-Encoding'] = 'gzip';
    
        return new Response(object.body, { headers });
      }
    
      return new Response('Invalid APT repository path structure', { status: 400 });
    }

    if (url.pathname.startsWith('/archlinux/')) {
      const path = url.pathname.replace('/archlinux/', '');

      const key = path
        .replace(/\.db(?=($|\.))/, '.db.tar.gz')
        .replace(/\.files(?=($|\.))/, '.files.tar.gz');

      const object = await env.BUCKET.get(key);
      if (!object) {
        return new Response(`${key} not found`, { status: 404 });
      }

      // MIME types
      let contentType = 'application/octet-stream';
      if (key.endsWith('.zst')) contentType = 'application/zstd';
      
      return new Response(object.body, {
        headers: {
          'Content-Type': contentType,
          'Content-Length': object.size.toString(),
          'Cache-Control': 'public, max-age=3600',
        },
      });
    }

    // List files and generate HTML
    const files = await listFiles(env.BUCKET);
    const html = generateHtml(files);

    return new Response(html, {
      headers: { 'Content-Type': 'text/html' },
    });
  },
}; 