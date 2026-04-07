import API from '../api.js';
import Components from '../components.js';

const Registry = {
    async render() {
        const user = await API.getMe();
        if (user.role !== 'superadmin') {
            return `<div class="card badge-error">Access Denied: Superadmin role required for registry settings.</div>`;
        }

        const registry = await API.get('/api/v1/admin/registry');
        
        return `
            <div class="page-header">
                <div>
                    <h1 class="page-title">Registry Settings</h1>
                    <p class="page-subtitle">Configure Docker image sources for readers and core services</p>
                </div>
                <div class="card-header-actions">
                    <button id="btn-save-registry" class="btn btn-primary">💾 Save Changes</button>
                </div>
            </div>

            <div class="grid grid-2">
                <div class="card">
                    <h2 class="card-title">⚙️ Deployment Mode</h2>
                    <div class="form-group">
                        <label>Registry Mode</label>
                        <select id="reg-mode">
                            <option value="github" ${registry.mode === 'github' ? 'selected' : ''}>GitHub Container Registry (GHCR)</option>
                            <option value="gitlab" ${registry.mode === 'gitlab' ? 'selected' : ''}>GitLab Container Registry</option>
                            <option value="custom" ${registry.mode === 'custom' ? 'selected' : ''}>Custom Full URLs</option>
                        </select>
                    </div>

                    <div id="github-settings" class="${registry.mode === 'github' ? '' : 'hidden'}">
                        <div class="form-group">
                            <label>GitHub Base URL (User/Org)</label>
                            <input type="text" id="github-base" value="${registry.github_base || ''}" placeholder="ghcr.io/sandun-s/qube-enterprise-home">
                            <small class="text-dim">Images resolved as: {base}/{suffix}:{arch}.latest</small>
                        </div>
                    </div>

                    <div id="gitlab-settings" class="${registry.mode === 'gitlab' ? '' : 'hidden'}">
                        <div class="form-group">
                            <label>GitLab Project Base</label>
                            <input type="text" id="gitlab-base" value="${registry.gitlab_base || ''}" placeholder="registry.gitlab.com/iot-team4/product">
                        </div>
                    </div>
                </div>

                <div class="card">
                    <h2 class="card-title">🔍 Image Resolution Preview</h2>
                    <div id="registry-preview" class="flex flex-col gap-10">
                        <!-- Preview of resolved image paths -->
                    </div>
                </div>
            </div>

            <div id="custom-overrides" class="card ${registry.mode === 'custom' ? '' : ''}">
                <h2 class="card-title">🖼️ Individual Image Overrides</h2>
                <div class="table-container">
                    <table>
                        <thead>
                            <tr>
                                <th>Container Type</th>
                                <th>Overridden Image URL</th>
                            </tr>
                        </thead>
                        <tbody id="registry-images">
                            ${this.renderImageRows(registry.images)}
                        </tbody>
                    </table>
                </div>
                <div class="text-dim text-center py-10" style="font-size: 11px;">
                    *Leave blank to use default resolution based on mode.
                </div>
            </div>

            <div class="card">
                <div class="flex-between">
                    <h2 class="card-title">📄 Raw Registry Data</h2>
                    <button class="btn btn-ghost btn-sm" id="btn-toggle-raw">👁️ Toggle Raw JSON</button>
                </div>
                <pre id="raw-registry-json" class="raw-json-preview hidden">${JSON.stringify(registry, null, 2)}</pre>
            </div>
        `;
    },

    async init() {
        this.bindEvents();
        this.updatePreview();
    },

    bindEvents() {
        const modeSelect = document.getElementById('reg-mode');
        const btnSave = document.getElementById('btn-save-registry');
        const btnToggleRaw = document.getElementById('btn-toggle-raw');

        modeSelect.addEventListener('change', (e) => {
            const mode = e.target.value;
            document.getElementById('github-settings').classList.toggle('hidden', mode !== 'github');
            document.getElementById('gitlab-settings').classList.toggle('hidden', mode !== 'gitlab');
            this.updatePreview();
        });

        const inputs = document.querySelectorAll('input');
        inputs.forEach(input => input.addEventListener('input', () => this.updatePreview()));

        btnSave.addEventListener('click', () => this.handleSave());
        btnToggleRaw.addEventListener('click', () => {
            const pre = document.getElementById('raw-registry-json');
            pre.classList.toggle('hidden');
        });
    },

    renderImageRows(images) {
        const types = [
            'img_conf_agent', 'img_influx_sql', 'img_modbus_reader', 
            'img_snmp_reader', 'img_mqtt_reader', 'img_opcua_reader', 
            'img_http_reader', 'img_bacnet_reader', 'img_lorawan_reader', 'img_dnp3_reader'
        ];
        
        return types.map(t => `
            <tr>
                <td style="font-family: var(--font-mono); font-weight: 600;">${t}</td>
                <td><input type="text" class="image-input" data-key="${t}" value="${images?.[t] || ''}" placeholder="Full URL or leave empty"></td>
            </tr>
        `).join('');
    },

    getResolvedImage(type, mode, githubBase, gitlabBase, overrides) {
        if (overrides[type]) return overrides[type];
        const suffix = type.replace('img_', '').replace('_', '-');
        
        if (mode === 'github') return `${githubBase}/${suffix}:arm64.latest`;
        if (mode === 'gitlab') return `${gitlabBase}/${suffix}:arm64.latest`;
        return `[NOT SET]`;
    },

    updatePreview() {
        const mode = document.getElementById('reg-mode').value;
        const githubBase = document.getElementById('github-base')?.value || '';
        const gitlabBase = document.getElementById('gitlab-base')?.value || '';
        const previewContainer = document.getElementById('registry-preview');
        
        const overrides = {};
        document.querySelectorAll('.image-input').forEach(input => {
            if (input.value) overrides[input.dataset.key] = input.value;
        });

        const types = ['img_conf_agent', 'img_modbus_reader', 'img_bacnet_reader'];
        
        previewContainer.innerHTML = types.map(t => `
            <div style="font-size: 11px;">
                <div class="text-dim">${t}</div>
                <div style="font-family: var(--font-mono); background: rgba(0,0,0,0.2); padding: 4px 8px; border-radius: 4px; border: 1px solid var(--border);">
                    ${this.getResolvedImage(t, mode, githubBase, gitlabBase, overrides)}
                </div>
            </div>
        `).join('');
    },

    async handleSave() {
        const btn = document.getElementById('btn-save-registry');
        const mode = document.getElementById('reg-mode').value;
        const body = {
            mode: mode,
            github_base: document.getElementById('github-base')?.value || '',
            gitlab_base: document.getElementById('gitlab-base')?.value || '',
            images: {}
        };

        document.querySelectorAll('.image-input').forEach(input => {
            if (input.value) body.images[input.dataset.key] = input.value;
        });

        try {
            btn.disabled = true;
            btn.textContent = 'Saving...';
            
            await API.put('/api/v1/admin/registry', body);

            Components.showAlert('Registry settings updated');
            window.location.reload();
        } catch (err) {
            Components.showAlert(err.message, 'error');
        } finally {
            btn.disabled = false;
            btn.textContent = '💾 Save Changes';
        }
    }
};

export default Registry;
