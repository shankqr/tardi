import { initializeApp, type FirebaseApp } from 'firebase/app';
import { getAuth, type Auth } from 'firebase/auth';

const firebaseConfig = {
	apiKey: import.meta.env.VITE_FIREBASE_API_KEY || '',
	authDomain: import.meta.env.VITE_FIREBASE_AUTH_DOMAIN || '',
	projectId: import.meta.env.VITE_FIREBASE_PROJECT_ID || '',
	storageBucket: import.meta.env.VITE_FIREBASE_STORAGE_BUCKET || '',
	messagingSenderId: import.meta.env.VITE_FIREBASE_MESSAGING_SENDER_ID || '',
	appId: import.meta.env.VITE_FIREBASE_APP_ID || ''
};

let app: FirebaseApp;
let auth: Auth;

export function getFirebaseApp(): FirebaseApp {
	if (!app) {
		app = initializeApp(firebaseConfig);
	}
	return app;
}

export function getFirebaseAuth(): Auth {
	if (!auth) {
		auth = getAuth(getFirebaseApp());
	}
	return auth;
}
