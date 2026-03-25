/**
 * Re-export loginWithCredentials as loginAs for backward compatibility.
 * New tests should import directly from './auth' instead.
 */
export { loginWithCredentials as loginAs } from './auth';
