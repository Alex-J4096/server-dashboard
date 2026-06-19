export interface ServerStatus{state:'stopped'|'starting'|'running'|'stopping'|'crashed';pid?:number;run_id?:number;started_at?:string;uptime_seconds:number}
export interface LogEntry{id?:number;server_id:string;run_id?:number;level:string;source:string;message:string;created_at:string}
export interface MetricsSummary{server:ServerStatus;process:{cpu_percent:number;memory_bytes:number};system:{cpu_percent:number;memory_total:number;memory_used:number;disk_total:number;disk_used:number};network:{rx_bytes_total:number;tx_bytes_total:number}}
export interface Snapshot{id:number;file_path:string;content_hash:string;reason?:string;created_at:string}
export type Role='admin'|'operator'|'viewer'
export interface User{id:string;username:string;role:Role;disabled:boolean;created_at:string}
