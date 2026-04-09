import API from './api.js';
import Components from './components.js';

// Import Page Handlers (to be created)
// import Dashboard from './pages/dashboard.js';
// ...

const App = {
    state: {
        user: null,
        currentPage: 'dashboard',
    },

    async init() {
        console.log('Qube v2 App Initializing...');
        console.log('API Base URL:', API.baseUrl);
        this.bindEvents();
        
        if (API.token) {
            await this.checkAuth();
        } else {
            this.showAuth();
        }

        // Listen for hash changes
        window.addEventListener('hashchange', () => this.route());
        this.route();
    },

    async checkAuth() {
        try {
            const user = await API.getMe();
            this.state.user = user;
            API.setUserRole(user.role || '');
            this.updateUserUI(user);
            this.showApp();
            this.initWS(); // Start WebSocket Hub
        } catch (err) {
            console.error('Auth verification failed', err);
            this.showAuth();
        }
    },

    initWS() {
        if (this.ws) this.ws.close();
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        
        // Use API.baseUrl as the source of truth for the host/port
        // API.baseUrl is typically http://hostname:8080
        let host = window.location.host;
        try {
            const apiHost = new URL(API.baseUrl).host;
            if (apiHost) host = apiHost;
        } catch (e) {
            console.warn('Could not parse API.baseUrl for WebSocket', e);
        }

        const wsUrl = `${protocol}//${host}/ws/dashboard?token=${API.token}`;
        
        this.ws = new WebSocket(wsUrl);
        this.ws.onmessage = (e) => {
            try { this.handleWSMessage(JSON.parse(e.data)); } catch (err) { console.error('WS Error', err); }
        };
        this.ws.onopen = () => document.getElementById('sync-status')?.classList.remove('hidden');
        this.ws.onclose = () => {
            document.getElementById('sync-status')?.classList.add('hidden');
            setTimeout(() => this.initWS(), 5000);
        };
    },

    handleWSMessage(msg) {
        if (msg.type === 'config_update') {
            Components.showAlert(`Qube ${msg.qube_id} synced!`, 'blue');
            if (this.state.currentPage === 'fleet') this.loadPage('fleet');
        }
        if (msg.type === 'sensor_reading') {
            window.dispatchEvent(new CustomEvent('sensor-data', { detail: msg }));
        }
    },

    updateUserUI(user) {
        document.getElementById('user-email').textContent = user.email || 'unknown';
        const roleBadge = document.getElementById('user-role');
        const role = (user.role || 'viewer').toLowerCase();
        roleBadge.textContent = role.toUpperCase();
        roleBadge.className = `badge badge-${role === 'superadmin' ? 'warning' : role === 'admin' ? 'success' : 'blue'}`;
        
        // Role-based visibility (superadmin-only pages)
        document.querySelectorAll('[data-page="registry"], [data-page="reader-templates"], [data-page="admin-qubes"]').forEach(el => {
            el.classList.toggle('hidden', user.role !== 'superadmin');
        });
    },

    showAuth() {
        document.getElementById('auth-view').classList.remove('hidden');
        document.getElementById('sidebar').classList.add('hidden');
        document.getElementById('main-content').classList.add('hidden');
    },

    showApp() {
        document.getElementById('auth-view').classList.add('hidden');
        document.getElementById('sidebar').classList.remove('hidden');
        document.getElementById('main-content').classList.remove('hidden');
    },

    async handleLogin() {
        const email = document.getElementById('auth-email').value;
        const password = document.getElementById('auth-password').value;
        const errorEl = document.getElementById('auth-error');
        
        try {
            console.log('Attempting login for:', email);
            errorEl.classList.add('hidden');
            const res = await API.login(email, password);
            console.log('Login success, checking auth...');
            API.setToken(res.token);
            await this.checkAuth();
            this.route(); // Trigger page load after auth
            Components.showAlert('Logged in successfully');
        } catch (err) {
            errorEl.textContent = err.message;
            errorEl.classList.remove('hidden');
        }
    },
    
    async handleDevLogin() {
        document.getElementById('auth-email').value = 'iotteam@internal.local';
        document.getElementById('auth-password').value = 'iotteam2024';
        this.handleLogin();
    },

    async handleRegister() {
        const orgName = document.getElementById('reg-name').value;
        const email = document.getElementById('reg-email').value;
        const password = document.getElementById('reg-password').value;
        const errorEl = document.getElementById('auth-error');
        
        try {
            errorEl.classList.add('hidden');
            const res = await API.register(orgName, email, password);
            API.setToken(res.token);
            await this.checkAuth();
            this.route(); // Trigger page load after auth
            Components.showAlert('Account created successfully');
        } catch (err) {
            errorEl.textContent = err.message;
            errorEl.classList.remove('hidden');
        }
    },

    bindEvents() {
        // Auth Toggles
        document.getElementById('toggle-register')?.addEventListener('click', () => {
            document.getElementById('login-form').classList.add('hidden');
            document.getElementById('register-form').classList.remove('hidden');
        });
        document.getElementById('toggle-login')?.addEventListener('click', () => {
            document.getElementById('login-form').classList.remove('hidden');
            document.getElementById('register-form').classList.add('hidden');
        });

        // Action Buttons
        document.getElementById('btn-login')?.addEventListener('click', () => this.handleLogin());
        document.getElementById('btn-register')?.addEventListener('click', () => this.handleRegister());
        document.getElementById('btn-dev-superadmin')?.addEventListener('click', () => this.handleDevLogin());
        document.getElementById('logout-btn')?.addEventListener('click', () => API.logout());

        // Keyboard support (Enter key)
        const loginInputs = ['auth-email', 'auth-password'];
        loginInputs.forEach(id => {
            document.getElementById(id)?.addEventListener('keypress', (e) => {
                if (e.key === 'Enter') this.handleLogin();
            });
        });

        const regInputs = ['reg-name', 'reg-email', 'reg-password'];
        regInputs.forEach(id => {
            document.getElementById(id)?.addEventListener('keypress', (e) => {
                if (e.key === 'Enter') this.handleRegister();
            });
        });

        // Sidebar Nav
        document.querySelectorAll('.nav-item').forEach(item => {
            item.addEventListener('click', (e) => {
                document.querySelectorAll('.nav-item').forEach(i => i.classList.remove('active'));
                item.classList.add('active');
            });
        });
    },

    async route() {
        const hash = window.location.hash.replace('#', '') || 'dashboard';
        if (!API.token && hash !== 'login') {
            // window.location.hash = '#login';
            return;
        }

        this.state.currentPage = hash;
        this.loadPage(hash);
    },

    async loadPage(page) {
        const contentEl = document.getElementById('content');
        contentEl.innerHTML = `<div class="text-center mt-20"><div class="badge badge-blue">Loading ${page}...</div></div>`;

        // Update active sidebar item
        document.querySelectorAll('.nav-item').forEach(item => {
            item.classList.toggle('active', item.dataset.page === page);
        });

        try {
            // Dynamically import the page logic
            const module = await import(`./pages/${page}.js`).catch(e => {
                console.error(`Failed to load page module: ${page}`, e);
                return { default: { render: () => `<h2>Page Not Found: ${page}</h2>` } };
            });

            if (module.default && module.default.render) {
                const html = await module.default.render();
                contentEl.innerHTML = html;
                if (module.default.init) await module.default.init();
            }
        } catch (err) {
            contentEl.innerHTML = `<div class="card badge-error">${err.message}</div>`;
        }
    }
};

window.onload = () => App.init();

export default App;
