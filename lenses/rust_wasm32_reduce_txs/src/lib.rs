// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

use std::error::Error;
use serde::Deserialize;
use serde::Serialize;
use lens_sdk::StreamOption;
use lens_sdk::option::StreamOption::{Some, None, EndOfStream};

#[link(wasm_import_module = "lens")]
extern "C" {
    fn next() -> *mut u8;
}

#[derive(Deserialize)]
pub struct Input {
    pub topics: String, // data is transaction input data
    pub from: String, // calling address
    pub to: String, // maybe contract address
    pub address: String, // contract address within the log view this may need to be nested?
}

#[derive(Serialize)]
pub struct Output {
    pub topic0: String,
    pub topic1: String,
    pub topic2: String,
    pub topic3: String,
    pub topic4: String,
    pub from: String, // this may not be needed. 
    pub to: String,
    pub contractAddress: String, // if different from to
}

#[no_mangle]
pub extern fn alloc(size: usize) -> *mut u8 {
    lens_sdk::alloc(size)
}

#[no_mangle]
pub extern fn transform() -> *mut u8 {
    match try_transform() {
        Ok(o) => match o {
            Some(result_json) => lens_sdk::to_mem(lens_sdk::JSON_TYPE_ID, &result_json),
            None => lens_sdk::nil_ptr(),
            EndOfStream => lens_sdk::to_mem(lens_sdk::EOS_TYPE_ID, &[]),
        },
        Err(e) => lens_sdk::to_mem(lens_sdk::ERROR_TYPE_ID, &e.to_string().as_bytes())
    }
}

fn try_transform() -> Result<StreamOption<Vec<u8>>, Box<dyn Error>> {
    loop {
        let ptr = unsafe { next() };
        let input = match lens_sdk::try_from_mem::<Input>(ptr)? {
            Some(v) => v,
            // Implementations of `transform` are free to handle nil however they like. In this
            // implementation we chose to return nil given a nil input.
            None => return Ok(None),
            EndOfStream => return Ok(EndOfStream)
        };

        let res = input.topics.split(";").collect::<Vec<&str>>();
        let mut result = Output {
            from: input.from,
            to: input.to,
            contractAddress: input.address,
            topic0 : "0x0000000000000000000000000000000000000000000000000000000000000000".to_string(),
            topic1 : "0x0000000000000000000000000000000000000000000000000000000000000000".to_string(),
            topic2 : "0x0000000000000000000000000000000000000000000000000000000000000000".to_string(),
            topic3 : "0x0000000000000000000000000000000000000000000000000000000000000000".to_string(),
            topic4 : "0x0000000000000000000000000000000000000000000000000000000000000000".to_string(),
        };
        for i in 0..res.len() {
            match i {
                0 => result.topic0 = res[i].to_string(),
                1 => result.topic1 = res[i].to_string(),
                2 => result.topic2 = res[i].to_string(),
                3 => result.topic3 = res[i].to_string(),
                4 => result.topic4 = res[i].to_string(),
                _ => break
            }
        }


        
        let result_json = serde_json::to_vec(&result)?;
        lens_sdk::free_transport_buffer(ptr)?;
        return Ok(Some(result_json))
    }
}
