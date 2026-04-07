import API from '../api.js';
import Components from '../components.js';

const Protocols = {
    async render() {
        return `
            <div class="page-header">
                <div>
                    <h2 class="page-title">Supported Protocols</h2>
                    <p class="page-subtitle">Available communication standards and reader templates</p>
                </div>
            </div>

            <div id="protocols-grid" class="grid grid-2">
                <div class="text-center page-subtitle">Loading system protocols...</div>
            </div>
        `;
    },

    async init() {
        this.loadProtocols();
    },

    async loadProtocols() {
        try {
            const protocols = await API.getProtocols();
            const grid = document.getElementById('protocols-grid');
            grid.innerHTML = '';
            
            const rawContainer = document.createElement('div');
            rawContainer.className = 'card';
            rawContainer.style.gridColumn = '1 / -1';
            rawContainer.innerHTML = `
                <div class="flex-between">
                    <h3 class="card-title">📜 All Protocols Data</h3>
                    <button class="btn btn-ghost btn-sm" id="btn-toggle-protocols-raw">👁️ Toggle Raw JSON</button>
                </div>
                <pre id="protocols-raw-json" class="raw-json-preview hidden">${JSON.stringify(protocols, null, 2)}</pre>
            `;
            grid.appendChild(rawContainer);

            document.getElementById('btn-toggle-protocols-raw').onclick = () => {
                document.getElementById('protocols-raw-json').classList.toggle('hidden');
            };

            for (const p of protocols) {
                const rTemplates = await API.getReaderTemplates(p.id);
                const rt = rTemplates[0] || { name: 'Generic Reader', description: 'Default container' };

                const card = document.createElement('div');
                card.className = 'card';
                card.innerHTML = `
                    <div class="flex-between mb-20" style="margin-bottom: 24px;">
                        <div class="flex">
                            <div class="logo-icon" style="background: var(--secondary); font-size: 14px;">🔌</div>
                            <div>
                                <div style="font-weight: 700; font-size: 16px;">${p.label}</div>
                                <div class="badge badge-blue" style="font-size: 9px;">${p.id}</div>
                            </div>
                        </div>
                        <div class="badge badge-${p.is_active ? 'success' : 'error'}">${p.is_active ? 'ACTIVE' : 'INACTIVE'}</div>
                    </div>
                    
                    <p class="page-subtitle" style="margin-bottom: 20px;">${p.description}</p>
                    
                    <div style="background: rgba(0,0,0,0.1); padding: 16px; border-radius: 12px; border: 1px solid var(--border);">
                        <div class="section-label" style="font-size: 10px; margin-bottom: 8px;">Reader Standard: <b>${p.reader_standard}</b></div>
                        <div style="font-weight: 600; font-size: 13px;">${rt.name}</div>
                        <div class="page-subtitle" style="font-size: 11px;">Image: ghcr.io/.../${rt.image_suffix}:arm64.latest</div>
                    </div>
                `;
                grid.appendChild(card);
            }
        } catch (err) {
            Components.showAlert(err.message, 'error');
        }
    }
};

export default Protocols;
